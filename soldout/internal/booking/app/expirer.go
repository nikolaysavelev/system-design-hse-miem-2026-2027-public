package app

import (
	"context"
	"log/slog"
	"time"
)

// Gauge — приёмник значения holds_active (metrics.HoldsActive).
type Gauge interface{ Set(float64) }

// Expirer раз в interval переводит просроченные active hold в released, истекает заказы с такими hold
// и обновляет holds_active. На занятии 7 заменяется таймером Temporal.
type Expirer struct {
	store    Store
	interval time.Duration
	gauge    Gauge
	logger   *slog.Logger
}

// NewExpirer собирает expirer.
func NewExpirer(store Store, interval time.Duration, gauge Gauge, logger *slog.Logger) *Expirer {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Expirer{store: store, interval: interval, gauge: gauge, logger: logger}
}

// Run блокируется до отмены ctx.
func (e *Expirer) Run(ctx context.Context) {
	t := time.NewTicker(e.interval)
	defer t.Stop()
	for {
		e.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (e *Expirer) tick(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, e.interval)
	defer cancel()
	now := time.Now()
	holds, err := e.store.ExpireHolds(ctx, now)
	if err != nil {
		e.logger.ErrorContext(ctx, "expirer: hold", "err", err)
		return
	}
	orders, err := e.store.ExpireOrders(ctx, now)
	if err != nil {
		e.logger.ErrorContext(ctx, "expirer: orders", "err", err)
		return
	}
	if holds > 0 || orders > 0 {
		e.logger.InfoContext(ctx, "expirer", "holds_released", holds, "orders_expired", orders)
	}
	if e.gauge != nil {
		if n, err := e.store.CountActiveHolds(ctx); err == nil {
			e.gauge.Set(float64(n))
		}
	}
}
