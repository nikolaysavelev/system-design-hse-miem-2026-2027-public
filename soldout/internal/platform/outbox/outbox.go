// Package outbox — transactional outbox (занятие 4).
//
// Событие, потеря которого нарушает инвариант или обязательство перед пользователем, пишется в таблицу outbox
// в той же транзакции, что и изменение агрегата (constitution §4). Доставку делает не код издателя:
//   - CDC (основной режим): Debezium читает INSERT из WAL (publication soldout_outbox) и публикует в Kafka через
//     EventRouter; строка удаляется в той же транзакции (DeleteAfterInsert), таблица всегда пуста;
//   - polling (lite, вариант A ADR-003): строка живёт, пока Poller её не отдаст потребителю и не удалит.
//
// Контекст трейса (W3C traceparent) сохраняется в строке и едет заголовком сообщения: трейс не рвётся на брокере.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/platform/db"
	"github.com/nikolaysavelev/soldout/internal/platform/otel"
)

// Event — событие для outbox. Payload — само значение события (json.Marshal).
type Event interface {
	OutboxID() uuid.UUID   // id события: заголовок id в Kafka, ключ дедупликации у потребителя
	AggregateType() string // "order" → топик outbox.event.order
	AggregateID() string   // ключ сообщения Kafka: порядок гарантирован внутри агрегата (партиции)
	EventType() string     // "OrderPaid": заголовок type
}

// Options — режим записи.
type Options struct {
	// DeleteAfterInsert: удалить строку в той же транзакции (CDC: в WAL остаётся INSERT). false — для polling relay.
	DeleteAfterInsert bool
}

// Writer пишет события в outbox внутри транзакции вызывающего.
type Writer struct{ opts Options }

// NewWriter создаёт писателя.
func NewWriter(opts Options) *Writer { return &Writer{opts: opts} }

// Append записывает событие в транзакции tx. Вызывать только внутри транзакции изменения агрегата:
// commit делает событие видимым для доставки, rollback — отменяет его вместе с изменением.
func (w *Writer) Append(ctx context.Context, tx db.DBTX, e Event) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("outbox: marshal %s: %w", e.EventType(), err)
	}
	var traceparent *string
	if tp := otel.Traceparent(ctx); tp != "" {
		traceparent = &tp
	}
	if _, err := tx.Exec(ctx, `/* outbox.insert */
		INSERT INTO outbox (id, aggregatetype, aggregateid, type, payload, traceparent) VALUES ($1, $2, $3, $4, $5, $6)`,
		e.OutboxID(), e.AggregateType(), e.AggregateID(), e.EventType(), payload, traceparent); err != nil {
		return fmt.Errorf("outbox: insert: %w", err)
	}
	if w.opts.DeleteAfterInsert {
		if _, err := tx.Exec(ctx, `/* outbox.delete */ DELETE FROM outbox WHERE id = $1`, e.OutboxID()); err != nil {
			return fmt.Errorf("outbox: delete: %w", err)
		}
	}
	otel.Event(ctx, "outbox.append", otel.String("messaging.message.id", e.OutboxID().String()),
		otel.String("outbox.type", e.EventType()), otel.String("outbox.aggregate_id", e.AggregateID()))
	return nil
}

// Record — строка outbox, прочитанная relay'ем.
type Record struct {
	ID            uuid.UUID
	AggregateType string
	AggregateID   string
	Type          string
	Payload       []byte
	Traceparent   string
	CreatedAt     time.Time
}

// Poller — relay для lite-режима: читает outbox по порядку записи и удаляет обработанные строки.
// Один экземпляр на базу (без блокировок строк: обработка идёт дольше idle_in_transaction_session_timeout);
// повторная доставка после падения возможна, потребитель идемпотентен.
type Poller struct {
	pool     *db.Pool
	batch    int
	interval time.Duration
}

// NewPoller создаёт relay.
func NewPoller(pool *db.Pool, batch int, interval time.Duration) *Poller {
	if batch <= 0 {
		batch = 100
	}
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	return &Poller{pool: pool, batch: batch, interval: interval}
}

// Run читает пачки и отдаёт их handle; строки удаляются после успешной обработки пачки. Завершается по ctx.
func (p *Poller) Run(ctx context.Context, handle func(ctx context.Context, batch []Record) error) error {
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		recs, err := p.fetch(ctx)
		if err == nil && len(recs) > 0 {
			if err = handle(ctx, recs); err == nil {
				err = p.ack(ctx, recs)
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		if err != nil || len(recs) < p.batch {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
			}
		}
	}
}

func (p *Poller) fetch(ctx context.Context) ([]Record, error) {
	rows, err := p.pool.Query(ctx, `/* outbox.poll */
		SELECT id, aggregatetype, aggregateid, type, payload, coalesce(traceparent, ''), created_at
		FROM outbox ORDER BY created_at LIMIT $1`, p.batch)
	if err != nil {
		return nil, fmt.Errorf("outbox: poll: %w", err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.AggregateType, &r.AggregateID, &r.Type, &r.Payload, &r.Traceparent, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("outbox: poll scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Poller) ack(ctx context.Context, recs []Record) error {
	ids := make([]uuid.UUID, len(recs))
	for i, r := range recs {
		ids[i] = r.ID
	}
	if _, err := p.pool.Exec(ctx, `/* outbox.ack */ DELETE FROM outbox WHERE id = ANY($1)`, ids); err != nil {
		return fmt.Errorf("outbox: ack: %w", err)
	}
	return nil
}
