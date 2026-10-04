package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	bookingapi "github.com/nikolaysavelev/soldout/internal/booking/api"
	"github.com/nikolaysavelev/soldout/internal/notification/api"
)

type fakeStore struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*api.Notification
	byOK map[string]*api.Notification
}

func newStore() *fakeStore {
	return &fakeStore{byID: map[uuid.UUID]*api.Notification{}, byOK: map[string]*api.Notification{}}
}

func (s *fakeStore) Accept(_ context.Context, _ string, n api.Notification) (api.Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := n.OrderID.String() + n.Kind
	if cur, ok := s.byOK[key]; ok {
		return *cur, nil
	}
	c := n
	s.byOK[key], s.byID[n.ID] = &c, &c
	return c, nil
}

func (s *fakeStore) SetStatus(_ context.Context, id uuid.UUID, status string, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[id].Status = status
	return nil
}

type fakeSender struct {
	mu    sync.Mutex
	calls int
	fail  int // сколько первых вызовов упадут
}

func (f *fakeSender) Send(context.Context, api.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.fail {
		return errors.New("502")
	}
	return nil
}

type fakeDLQ struct{ msgs []Message }

func (d *fakeDLQ) Publish(_ context.Context, m Message, _ error, _ int) error {
	d.msgs = append(d.msgs, m)
	return nil
}

type nopMetrics struct{}

func (nopMetrics) Consumed()                   {}
func (nopMetrics) Duplicate()                  {}
func (nopMetrics) Failed()                     {}
func (nopMetrics) DeadLettered()               {}
func (nopMetrics) DeliveryDelay(time.Duration) {}

func newProc(s Store, snd Sender, d DLQ) *Processor {
	return New(s, snd, d, nopMetrics{}, Options{MaxAttempts: 5, Backoff: []time.Duration{time.Millisecond}},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func orderPaid(t *testing.T) Message {
	t.Helper()
	ev := bookingapi.OrderPaidEvent{ID: uuid.New(), OrderID: uuid.New(), EventID: uuid.New(), UserID: uuid.New(),
		SeatIDs: []int64{40777}, AmountMinor: 500000, At: time.Now()}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return Message{Topic: "outbox.event.order", Key: ev.OrderID.String(), Value: b, Headers: map[string]string{"id": ev.ID.String(), "type": "OrderPaid"}}
}

func TestDuplicateDeliverySendsOnce(t *testing.T) {
	store, sender, dlq := newStore(), &fakeSender{}, &fakeDLQ{}
	p := newProc(store, sender, dlq)
	m := orderPaid(t)
	for i := 0; i < 3; i++ { // at-least-once: то же событие трижды
		if err := p.Handle(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.byOK) != 1 {
		t.Fatalf("ожидали одно уведомление, получили %d", len(store.byOK))
	}
	if sender.calls != 1 {
		t.Fatalf("письмо отправлено %d раз, ожидали 1", sender.calls)
	}
	if len(dlq.msgs) != 0 {
		t.Fatal("валидное сообщение не должно попадать в DLQ")
	}
}

func TestSendRetriedUntilSuccess(t *testing.T) {
	store, sender, dlq := newStore(), &fakeSender{fail: 2}, &fakeDLQ{}
	if err := newProc(store, sender, dlq).Handle(context.Background(), orderPaid(t)); err != nil {
		t.Fatal(err)
	}
	for _, n := range store.byOK {
		if n.Status != api.StatusSent {
			t.Fatalf("после повторов статус %q, ожидали sent", n.Status)
		}
	}
	if sender.calls != 3 || len(dlq.msgs) != 0 {
		t.Fatalf("вызовов %d (ожидали 3), в DLQ %d", sender.calls, len(dlq.msgs))
	}
}

func TestPoisonGoesToDLQAfterMaxAttempts(t *testing.T) {
	store, sender, dlq := newStore(), &fakeSender{}, &fakeDLQ{}
	p := newProc(store, sender, dlq)
	var attempts int
	p.sleep = func(context.Context, time.Duration) error { attempts++; return nil }
	poison := Message{Topic: "outbox.event.order", Offset: 42, Key: "poison", Value: []byte(`{"order_id": "не-uuid"`)}
	if err := p.Handle(context.Background(), poison); err != nil {
		t.Fatalf("после DLQ сообщение считается обработанным (смещение коммитится), получили %v", err)
	}
	if len(dlq.msgs) != 1 || attempts != 4 {
		t.Fatalf("в DLQ %d сообщений, пауз %d (ожидали 1 и 4: 5 попыток)", len(dlq.msgs), attempts)
	}
	if sender.calls != 0 || len(store.byOK) != 0 {
		t.Fatal("невалидное сообщение не должно доходить до записи и отправки")
	}
	h := DLQHeaders(poison, ErrPoison, 5)
	if h["attempts"] != "5" || h["original_offset"] != "42" || h["error"] == "" {
		t.Fatalf("заголовки DLQ: %v", h)
	}
}

func TestDebeziumStringPayload(t *testing.T) {
	m := orderPaid(t)
	quoted, _ := json.Marshal(string(m.Value)) // JsonConverter без expand.json.payload отдаёт строку
	m.Value = quoted
	if _, err := decode(m); err != nil {
		t.Fatalf("payload-строка не разобрана: %v", err)
	}
}
