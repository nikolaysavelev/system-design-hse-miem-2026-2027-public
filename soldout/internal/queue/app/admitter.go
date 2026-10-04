package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/queue/domain"
)

// Queue — операции waiting room в Valkey (реализация в adapters/valkey).
type Queue interface {
	// Enqueue ставит пользователя в очередь (идемпотентно, ZADD NX) и возвращает позицию (1 = первый).
	Enqueue(ctx context.Context, eventID, userID uuid.UUID, now time.Time) (int64, error)
	// Position — позиция в очереди (0 — не в очереди).
	Position(ctx context.Context, eventID, userID uuid.UUID) (int64, error)
	// Admit атомарно (Lua): пополняет token bucket по rate, забирает до k пользователей из головы очереди
	// в inflight и возвращает их. now — миллисекунды.
	Admit(ctx context.Context, eventID uuid.UUID, nowMs int64, rate float64, k int) ([]uuid.UUID, error)
	// Done убирает пользователя из inflight после выдачи допуска.
	Done(ctx context.Context, eventID, userID uuid.UUID) error
	// Reap возвращает залежавшихся в inflight (старше olderThanMs) в голову очереди; возвращает число.
	Reap(ctx context.Context, eventID uuid.UUID, nowMs, olderThanMs int64) (int64, error)
	// Sizes — размер очереди и inflight.
	Sizes(ctx context.Context, eventID uuid.UUID) (queue, inflight int64, err error)
	// Events — мероприятия с активной очередью.
	Events(ctx context.Context) ([]uuid.UUID, error)
	// SetAdmitted/GetAdmitted — выданный допуск пользователя (токен), TTL = срок допуска.
	SetAdmitted(ctx context.Context, eventID, userID uuid.UUID, token string, ttl time.Duration) error
	GetAdmitted(ctx context.Context, eventID, userID uuid.UUID) (string, error)
}

// QueueMetrics — метрики waiting room (metrics.Queue*).
type QueueMetrics interface {
	SetSizes(eventID string, queue, inflight float64)
	Admitted(n int)
	SetRate(rate float64)
}

// AdmitterOptions — настройки admitter'а.
type AdmitterOptions struct {
	Rate     float64       // допусков в секунду (ADMISSION_RATE)
	Batch    int           // не больше k за тик
	Interval time.Duration // тик (100 мс)
	Reap     time.Duration // inflight старше этого возвращается в очередь (10 с)
	TTL      time.Duration // срок допуска
	Metrics  QueueMetrics
}

// Admitter — один Lua-скрипт на тик: token bucket → ZPOPMIN → inflight → выдача допуска → ZREM inflight.
// Rate глобальный только при одном инстансе (ADR-002, Revisit when).
type Admitter struct {
	svc    *Service
	queue  Queue
	opts   AdmitterOptions
	logger *slog.Logger
}

// NewAdmitter собирает admitter.
func NewAdmitter(svc *Service, queue Queue, opts AdmitterOptions, logger *slog.Logger) *Admitter {
	if opts.Rate <= 0 {
		opts.Rate = 50
	}
	if opts.Batch <= 0 {
		opts.Batch = 50
	}
	if opts.Interval <= 0 {
		opts.Interval = 100 * time.Millisecond
	}
	if opts.Reap <= 0 {
		opts.Reap = 10 * time.Second
	}
	if opts.TTL <= 0 {
		opts.TTL = 10 * time.Minute
	}
	return &Admitter{svc: svc, queue: queue, opts: opts, logger: logger}
}

// Run блокируется до отмены ctx.
func (a *Admitter) Run(ctx context.Context) {
	if a.opts.Metrics != nil {
		a.opts.Metrics.SetRate(a.opts.Rate)
	}
	t := time.NewTicker(a.opts.Interval)
	defer t.Stop()
	reap := time.NewTicker(time.Second)
	defer reap.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.Tick(ctx)
		case <-reap.C:
			a.reap(ctx)
		}
	}
}

// Tick — один проход admitter'а по всем очередям (публичен для тестов).
func (a *Admitter) Tick(ctx context.Context) {
	events, err := a.queue.Events(ctx)
	if err != nil {
		a.logger.ErrorContext(ctx, "admitter: events", "err", err)
		return
	}
	for _, ev := range events {
		users, err := a.queue.Admit(ctx, ev, time.Now().UnixMilli(), a.opts.Rate, a.opts.Batch)
		if err != nil {
			a.logger.ErrorContext(ctx, "admitter: admit", "event", ev, "err", err)
			continue
		}
		for _, u := range users {
			if err := a.admitOne(ctx, ev, u); err != nil {
				a.logger.ErrorContext(ctx, "admitter: выдача допуска", "event", ev, "user", u, "err", err)
				continue // останется в inflight — reaper вернёт в очередь
			}
		}
		if a.opts.Metrics != nil {
			a.opts.Metrics.Admitted(len(users))
			if q, inflight, err := a.queue.Sizes(ctx, ev); err == nil {
				a.opts.Metrics.SetSizes(ev.String(), float64(q), float64(inflight))
			}
		}
	}
}

func (a *Admitter) admitOne(ctx context.Context, eventID, userID uuid.UUID) error {
	adm := domain.NewAdmission(eventID, userID, a.svc.now(), a.opts.TTL)
	if err := a.svc.store.Insert(ctx, adm); err != nil { // PostgreSQL — источник истины для выданных допусков
		return err
	}
	if err := a.svc.cache.Set(ctx, adm, a.opts.TTL); err != nil {
		a.logger.WarnContext(ctx, "admitter: кэш токена", "err", err)
	}
	if err := a.queue.SetAdmitted(ctx, eventID, userID, adm.Token, a.opts.TTL); err != nil {
		return err
	}
	return a.queue.Done(ctx, eventID, userID)
}

func (a *Admitter) reap(ctx context.Context) {
	events, err := a.queue.Events(ctx)
	if err != nil {
		return
	}
	for _, ev := range events {
		if n, err := a.queue.Reap(ctx, ev, time.Now().UnixMilli(), a.opts.Reap.Milliseconds()); err == nil && n > 0 {
			a.logger.WarnContext(ctx, "admitter: возвращены из inflight в очередь", "event", ev, "n", n)
		}
	}
}
