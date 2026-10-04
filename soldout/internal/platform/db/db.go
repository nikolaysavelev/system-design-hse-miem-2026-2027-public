// Package db — пул соединений PostgreSQL (pgx v5), миграции (golang-migrate) и seed.
package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // драйвер pgx5://
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nikolaysavelev/soldout/internal/platform/metrics"
	"github.com/nikolaysavelev/soldout/migrations"
	"github.com/nikolaysavelev/soldout/seed"
)

// DBTX — общий интерфейс пула и транзакции; адаптеры модулей пишут запросы против него.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func wrap(op string, err error) error { return fmt.Errorf("db: %s: %w", op, err) }

// Migrate применяет все up-миграции из каталога migrations/ (встроены в бинарник).
func Migrate(ctx context.Context, url string, logger *slog.Logger) error {
	return MigrateFS(ctx, url, migrations.FS, logger)
}

// MigrateFS применяет миграции из произвольной ФС (используется тестами).
func MigrateFS(ctx context.Context, url string, fsys fs.FS, logger *slog.Logger) error {
	src, err := iofs.New(fsys, ".")
	if err != nil {
		return fmt.Errorf("db: migrations source: %w", err)
	}
	// Драйвер не оборачивает файл миграции в транзакцию: один CREATE INDEX CONCURRENTLY на файл выполняется
	// вне transaction block (шаг A занятия 2). x-multi-statement не используем: он режет файл по «;» даже в комментариях.
	dbURL := "pgx5://" + strings.TrimPrefix(strings.TrimPrefix(url, "postgres://"), "postgresql://")
	m, err := migrate.NewWithSourceInstance("iofs", src, dbURL)
	if err != nil {
		return fmt.Errorf("db: migrate init: %w", err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	v, dirty, _ := m.Version()
	if logger != nil {
		logger.InfoContext(ctx, "миграции применены", "version", v, "dirty", dirty)
	}
	return nil
}

// Seed загружает seed/seed.sql, если в БД ещё нет мероприятий. seatsPerEvent передаётся в SQL через
// настройку сессии soldout.seats_per_event (lite-профиль уменьшает объём).
func Seed(ctx context.Context, pool *Pool, seatsPerEvent int, logger *slog.Logger) error {
	var n int
	if err := pool.QueryRow(ctx, "/* platform.seed_check */ SELECT count(*) FROM events").Scan(&n); err != nil {
		return fmt.Errorf("db: seed check: %w", err)
	}
	if n > 0 {
		return nil
	}
	sqlText, err := fs.ReadFile(seed.FS, "seed.sql")
	if err != nil {
		return fmt.Errorf("db: read seed: %w", err)
	}
	started := time.Now()
	err = WithTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL soldout.seats_per_event = '%d'", seatsPerEvent)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, string(sqlText))
		return err
	})
	if err != nil {
		return fmt.Errorf("db: seed: %w", err)
	}
	if logger != nil {
		logger.InfoContext(ctx, "seed загружен", "seats_per_event", seatsPerEvent, "took", time.Since(started).String())
	}
	return nil
}

// WithTx выполняет fn в транзакции: commit при nil, rollback при ошибке или панике.
func WithTx(ctx context.Context, pool *Pool, fn func(tx pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		if errors.Is(err, ErrBusy) {
			return err
		}
		return fmt.Errorf("db: begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit: %w", err)
	}
	return nil
}

// IsUniqueViolation — нарушение unique-ограничения с заданным именем (пусто — любое).
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return constraint == "" || pgErr.ConstraintName == constraint
	}
	return false
}

// queryTracer измеряет длительность запросов; имя запроса берётся из комментария /* name */ в начале SQL.
type queryTracer struct{ m *metrics.Metrics }

type traceKey struct{}

type traceData struct {
	name  string
	start time.Time
}

var queryNameRe = regexp.MustCompile(`^\s*/\*\s*([A-Za-z0-9_.-]+)\s*\*/`)

func (t *queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	name := "unnamed"
	if m := queryNameRe.FindStringSubmatch(data.SQL); m != nil {
		name = m[1]
	}
	return context.WithValue(ctx, traceKey{}, traceData{name: name, start: time.Now()})
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if d, ok := ctx.Value(traceKey{}).(traceData); ok {
		t.m.DBQueryDuration.WithLabelValues(d.name).Observe(time.Since(d.start).Seconds())
	}
}
