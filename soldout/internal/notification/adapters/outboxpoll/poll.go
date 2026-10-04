// Package outboxpoll — источник событий notifier без Kafka и Debezium (lite, вариант A ADR-003):
// строки outbox читаются polling'ом (platform/outbox.Poller) и удаляются после обработки.
package outboxpoll

import (
	"context"
	"sync"

	"github.com/nikolaysavelev/soldout/internal/notification/app"
	"github.com/nikolaysavelev/soldout/internal/platform/outbox"
)

// Source — источник на polling'е.
type Source struct {
	poller      *outbox.Poller
	concurrency int
}

// New создаёт источник.
func New(poller *outbox.Poller, concurrency int) *Source {
	if concurrency <= 0 {
		concurrency = 64
	}
	return &Source{poller: poller, concurrency: concurrency}
}

// Run — цикл до отмены ctx. Пачка обрабатывается параллельно и удаляется целиком после обработки.
func (s *Source) Run(ctx context.Context, handle func(context.Context, app.Message) error) error {
	return s.poller.Run(ctx, func(ctx context.Context, batch []outbox.Record) error {
		sem := make(chan struct{}, s.concurrency)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var first error
		for _, r := range batch {
			sem <- struct{}{}
			wg.Add(1)
			go func(r outbox.Record) {
				defer func() { <-sem; wg.Done() }()
				m := app.Message{Topic: "outbox", Key: r.AggregateID, Value: r.Payload, Timestamp: r.CreatedAt,
					Headers: map[string]string{"id": r.ID.String(), "type": r.Type, "traceparent": r.Traceparent}}
				if err := handle(ctx, m); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
				}
			}(r)
		}
		wg.Wait()
		return first
	})
}

// Publish — DLQ в lite-режиме: брокера нет, сообщение остаётся в логе и метрике (строка outbox удаляется).
type LogDLQ struct {
	Log func(ctx context.Context, m app.Message, cause error, attempts int)
}

// Publish — app.DLQ.
func (d LogDLQ) Publish(ctx context.Context, m app.Message, cause error, attempts int) error {
	d.Log(ctx, m, cause, attempts)
	return nil
}
