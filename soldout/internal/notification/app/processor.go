// Package app — обработка события OrderPaid в сервисе notifier: идемпотентный приём, отправка письма, повторы, DLQ.
//
// Доставка at-least-once (Kafka или polling outbox): одно событие может прийти несколько раз. Дедупликация —
// notifier.processed_events (id события) и уникальный (order_id, kind) в notifications, в одной транзакции.
// Письмо отправляется после commit приёма; повторная доставка уже отправленного письма ничего не делает,
// повторная доставка неотправленного (pending/failed) — отправляет снова.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	bookingapi "github.com/nikolaysavelev/soldout/internal/booking/api"
	"github.com/nikolaysavelev/soldout/internal/notification/api"
	"github.com/nikolaysavelev/soldout/internal/platform/otel"
)

// Message — сообщение источника (Kafka или строка outbox), без привязки к клиенту брокера.
type Message struct {
	Topic     string
	Partition int32
	Offset    int64
	Key       string
	Value     []byte
	Headers   map[string]string // id, type, traceparent
	Timestamp time.Time
}

// Sender — канал доставки (адаптер почтового шлюза).
type Sender interface {
	Send(ctx context.Context, n api.Notification) error
}

// Store — журнал уведомлений и обработанные события (adapters/pg).
type Store interface {
	// Accept в одной транзакции отмечает событие обработанным и создаёт уведомление pending (если его ещё нет).
	// Возвращает текущее уведомление по (order_id, kind): при повторной доставке — уже существующее.
	Accept(ctx context.Context, consumer string, n api.Notification) (api.Notification, error)
	SetStatus(ctx context.Context, id uuid.UUID, status string, attempts int) error
}

// DLQ — публикация необрабатываемого сообщения (топик outbox.event.order.dlq).
type DLQ interface {
	Publish(ctx context.Context, m Message, cause error, attempts int) error
}

// Metrics — счётчики процессора (реализация в cmd/notifier, Prometheus).
type Metrics interface {
	Consumed()
	Duplicate()
	Failed()
	DeadLettered()
	DeliveryDelay(d time.Duration) // от commit оплаты (OrderPaidEvent.At) до отправленного письма
}

// Options — политика повторов.
type Options struct {
	Consumer    string          // имя потребителя в processed_events: "notifier"
	MaxAttempts int             // NOTIFIER_MAX_ATTEMPTS, по умолчанию 5
	Backoff     []time.Duration // паузы между попытками; последняя повторяется
}

// Processor — обработчик событий OrderPaid.
type Processor struct {
	store   Store
	sender  Sender
	dlq     DLQ
	metrics Metrics
	opts    Options
	logger  *slog.Logger
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) error
}

// New собирает процессор.
func New(store Store, sender Sender, dlq DLQ, metrics Metrics, opts Options, logger *slog.Logger) *Processor {
	if opts.Consumer == "" {
		opts.Consumer = "notifier"
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}
	if len(opts.Backoff) == 0 {
		opts.Backoff = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}
	}
	return &Processor{store: store, sender: sender, dlq: dlq, metrics: metrics, opts: opts, logger: logger, now: time.Now, sleep: sleepCtx}
}

// ErrPoison — сообщение нельзя разобрать (невалидный payload или нет id события).
var ErrPoison = errors.New("notifier: невалидное сообщение")

// Handle обрабатывает одно сообщение: до MaxAttempts попыток с паузами, затем DLQ. Возвращает ошибку, только если
// сообщение не обработано и не отправлено в DLQ — тогда смещение коммитить нельзя (повтор после рестарта).
func (p *Processor) Handle(ctx context.Context, m Message) (err error) {
	// контекст продюсера из заголовка traceparent: один trace от POST /pay через Kafka до письма
	ctx = otel.Extract(ctx, m.Headers)
	ctx, span := otel.StartConsumer(ctx, "notifier.consume",
		otel.String("messaging.system", "kafka"), otel.String("messaging.destination.name", m.Topic),
		otel.Int64("messaging.kafka.offset", m.Offset), otel.Int("messaging.kafka.partition", int(m.Partition)),
		otel.String("messaging.message.id", m.Headers["id"]), otel.String("messaging.kafka.message.key", m.Key))
	defer otel.End(span, &err)
	p.metrics.Consumed()

	var last error
	for attempt := 1; attempt <= p.opts.MaxAttempts; attempt++ {
		if last = p.process(ctx, m, attempt); last == nil {
			return nil
		}
		p.metrics.Failed()
		p.logger.WarnContext(ctx, "notifier: попытка не удалась", "attempt", attempt, "offset", m.Offset, "err", last)
		if attempt < p.opts.MaxAttempts {
			if err := p.sleep(ctx, p.backoff(attempt)); err != nil {
				return err // остановка процесса: смещение не коммитим, событие придёт снова
			}
		}
	}
	otel.SetAttributes(ctx, otel.Bool("notifier.dead_lettered", true), otel.Int("retry.attempts", p.opts.MaxAttempts))
	if err := p.dlq.Publish(ctx, m, last, p.opts.MaxAttempts); err != nil {
		return fmt.Errorf("notifier: DLQ: %w (исходная ошибка: %v)", err, last)
	}
	p.metrics.DeadLettered()
	p.logger.ErrorContext(ctx, "notifier: сообщение отправлено в DLQ", "offset", m.Offset, "key", m.Key, "attempts", p.opts.MaxAttempts, "err", last)
	return nil
}

func (p *Processor) process(ctx context.Context, m Message, attempt int) error {
	ev, err := decode(m)
	if err != nil {
		return err
	}
	otel.SetUser(ctx, ev.UserID.String())
	n, err := p.store.Accept(ctx, p.opts.Consumer, api.Notification{
		ID: uuid.New(), OrderID: ev.OrderID, EventID: ev.ID, UserID: ev.UserID, Kind: api.KindTicketsIssued,
		Payload: map[string]any{"order_id": ev.OrderID.String(), "event_id": ev.EventID.String(), "seat_ids": ev.SeatIDs,
			"amount_minor": ev.AmountMinor, "link": "/v1/users/" + ev.UserID.String() + "/tickets"},
		Status: api.StatusPending, CreatedAt: p.now(),
	})
	if err != nil {
		return fmt.Errorf("notifier: приём события: %w", err)
	}
	if n.Status == api.StatusSent {
		p.metrics.Duplicate() // повторная доставка: письмо уже ушло, второй раз не шлём
		otel.SetAttributes(ctx, otel.Bool("notifier.duplicate", true))
		return nil
	}
	if err := p.send(ctx, n); err != nil {
		if serr := p.store.SetStatus(ctx, n.ID, api.StatusFailed, attempt); serr != nil {
			p.logger.ErrorContext(ctx, "notifier: статус failed не записан", "err", serr)
		}
		return err
	}
	if err := p.store.SetStatus(ctx, n.ID, api.StatusSent, attempt); err != nil {
		// письмо ушло, статус не записан: повторная доставка отправит ещё раз (at-least-once у почты)
		return fmt.Errorf("notifier: статус sent: %w", err)
	}
	p.metrics.DeliveryDelay(p.now().Sub(ev.At))
	return nil
}

func (p *Processor) send(ctx context.Context, n api.Notification) (err error) {
	ctx, span := otel.Start(ctx, "notification.Send", otel.String("notification.kind", n.Kind), otel.String("order.id", n.OrderID.String()))
	defer otel.End(span, &err)
	return p.sender.Send(ctx, n)
}

func (p *Processor) backoff(attempt int) time.Duration {
	if attempt-1 < len(p.opts.Backoff) {
		return p.opts.Backoff[attempt-1]
	}
	return p.opts.Backoff[len(p.opts.Backoff)-1]
}

// decode разбирает OrderPaidEvent. Debezium (expand.json.payload) отдаёт объект JSON; без expand — JSON-строку
// с объектом внутри; poller outbox — объект. Id события — заголовок id или поле id payload.
func decode(m Message) (bookingapi.OrderPaidEvent, error) {
	var ev bookingapi.OrderPaidEvent
	raw := m.Value
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ev, fmt.Errorf("%w: %v", ErrPoison, err)
		}
		raw = []byte(s)
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return ev, fmt.Errorf("%w: %v", ErrPoison, err)
	}
	if id, err := uuid.Parse(m.Headers["id"]); err == nil {
		ev.ID = id
	}
	if ev.ID == uuid.Nil || ev.OrderID == uuid.Nil || ev.UserID == uuid.Nil {
		return ev, fmt.Errorf("%w: нет id события, order_id или user_id", ErrPoison)
	}
	return ev, nil
}

// DLQHeaders — заголовки сообщения в DLQ: причина, число попыток и откуда пришло.
func DLQHeaders(m Message, cause error, attempts int) map[string]string {
	h := make(map[string]string, len(m.Headers)+5)
	for k, v := range m.Headers {
		h[k] = v
	}
	h["error"] = cause.Error()
	h["attempts"] = strconv.Itoa(attempts)
	h["original_topic"] = m.Topic
	h["original_partition"] = strconv.Itoa(int(m.Partition))
	h["original_offset"] = strconv.FormatInt(m.Offset, 10)
	return h
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
