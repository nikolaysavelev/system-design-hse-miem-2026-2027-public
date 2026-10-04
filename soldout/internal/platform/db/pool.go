package db

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nikolaysavelev/soldout/internal/platform/metrics"
)

// ErrBusy — не дождались свободного соединения за AcquireTimeout. HTTP-слой отдаёт 503 db_busy + Retry-After.
var ErrBusy = errors.New("db: пул занят, повторите позже")

// Pool — пул pgx с ограниченным ожиданием соединения. Под штормом очередь на соединение живёт здесь,
// а не в PostgreSQL (пул не ускоряет, он переносит очередь — закон Литтла), и мы отвечаем 503 быстро,
// вместо того чтобы держать клиента до таймаута сервера.
type Pool struct {
	pool           *pgxpool.Pool
	acquireTimeout time.Duration
	m              *metrics.Metrics
}

// PoolOptions — настройки пула.
type PoolOptions struct {
	MaxConns        int32
	MinConns        int32
	MaxConnIdleTime time.Duration
	MaxConnLifetime time.Duration
	AcquireTimeout  time.Duration
}

// NewPool открывает пул с настройками opts. Нулевые значения заменяются разумными дефолтами.
func NewPool(ctx context.Context, url string, opts PoolOptions, m *metrics.Metrics) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, wrap("parse url", err)
	}
	if opts.MaxConns <= 0 {
		opts.MaxConns = 40
	}
	if opts.MinConns < 0 {
		opts.MinConns = 0
	}
	if opts.MaxConnIdleTime <= 0 {
		opts.MaxConnIdleTime = 5 * time.Minute
	}
	if opts.MaxConnLifetime <= 0 {
		opts.MaxConnLifetime = 30 * time.Minute
	}
	if opts.AcquireTimeout <= 0 {
		opts.AcquireTimeout = 2 * time.Second
	}
	cfg.MaxConns = opts.MaxConns
	cfg.MinConns = opts.MinConns
	cfg.MaxConnIdleTime = opts.MaxConnIdleTime
	cfg.MaxConnLifetime = opts.MaxConnLifetime
	cfg.HealthCheckPeriod = 5 * time.Second
	if m != nil {
		cfg.ConnConfig.Tracer = &queryTracer{m: m}
	}
	raw, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, wrap("new pool", err)
	}
	// Ping с ретраем: PgBouncer после рестарта PostgreSQL несколько секунд отдаёт «server login has been failing»
	// (кэш DNS/ошибки логина), а при `compose up` пулер может стартовать раньше базы.
	var err2 error
	for attempt := 0; attempt < 10; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err2 = raw.Ping(pingCtx)
		cancel()
		if err2 == nil {
			break
		}
		select {
		case <-ctx.Done():
			raw.Close()
			return nil, wrap("ping", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
	if err2 != nil {
		raw.Close()
		return nil, wrap("ping", err2)
	}
	if m != nil {
		m.RegisterPool(raw)
	}
	return &Pool{pool: raw, acquireTimeout: opts.AcquireTimeout, m: m}, nil
}

// Raw — доступ к pgxpool (метрики, тесты).
func (p *Pool) Raw() *pgxpool.Pool { return p.pool }

// Close закрывает пул.
func (p *Pool) Close() { p.pool.Close() }

// Ping — проверка живости.
func (p *Pool) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// Stat — статистика пула.
func (p *Pool) Stat() *pgxpool.Stat { return p.pool.Stat() }

func (p *Pool) acquire(ctx context.Context) (*pgxpool.Conn, error) {
	start := time.Now()
	acqCtx, cancel := context.WithTimeout(ctx, p.acquireTimeout)
	defer cancel()
	conn, err := p.pool.Acquire(acqCtx)
	wait := time.Since(start)
	if p.m != nil {
		p.m.DBAcquireWait.Observe(wait.Seconds())
	}
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			if p.m != nil {
				p.m.DBAcquireTimeouts.Inc()
			}
			return nil, ErrBusy
		}
		return nil, wrap("acquire", err)
	}
	return conn, nil
}

// Exec выполняет команду.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	conn, err := p.acquire(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	defer conn.Release()
	return conn.Exec(ctx, sql, args...)
}

// Query выполняет запрос; соединение возвращается в пул при Close или по исчерпании строк.
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	conn, err := p.acquire(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := conn.Query(ctx, sql, args...)
	if err != nil {
		conn.Release()
		return nil, err
	}
	return &poolRows{Rows: rows, conn: conn}, nil
}

// QueryRow выполняет запрос одной строки; соединение возвращается после Scan.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	conn, err := p.acquire(ctx)
	if err != nil {
		return errRow{err: err}
	}
	return &poolRow{Row: conn.QueryRow(ctx, sql, args...), conn: conn}
}

// Begin открывает транзакцию; соединение возвращается после Commit/Rollback.
func (p *Pool) Begin(ctx context.Context) (pgx.Tx, error) {
	conn, err := p.acquire(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		return nil, err
	}
	return &poolTx{Tx: tx, conn: conn}, nil
}

type poolRows struct {
	pgx.Rows
	conn *pgxpool.Conn
	once sync.Once
}

func (r *poolRows) release() { r.once.Do(func() { r.conn.Release() }) }

func (r *poolRows) Next() bool {
	ok := r.Rows.Next()
	if !ok {
		r.release()
	}
	return ok
}

func (r *poolRows) Close() {
	r.Rows.Close()
	r.release()
}

type poolRow struct {
	pgx.Row
	conn *pgxpool.Conn
}

func (r *poolRow) Scan(dest ...any) error {
	defer r.conn.Release()
	return r.Row.Scan(dest...)
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

type poolTx struct {
	pgx.Tx
	conn *pgxpool.Conn
	once sync.Once
}

func (t *poolTx) release() { t.once.Do(func() { t.conn.Release() }) }

func (t *poolTx) Commit(ctx context.Context) error {
	defer t.release()
	return t.Tx.Commit(ctx)
}

func (t *poolTx) Rollback(ctx context.Context) error {
	defer t.release()
	return t.Tx.Rollback(ctx)
}
