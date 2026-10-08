package pg_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	bookingapi "github.com/nikolaysavelev/soldout/internal/booking/api"
	notificationpg "github.com/nikolaysavelev/soldout/internal/notification/adapters/pg"
	"github.com/nikolaysavelev/soldout/internal/notification/api"
	"github.com/nikolaysavelev/soldout/internal/notification/app"
	"github.com/nikolaysavelev/soldout/internal/platform/db"
)

type countingSender struct{ n atomic.Int32 }

func (s *countingSender) Send(context.Context, api.Notification) error {
	s.n.Add(1)
	time.Sleep(20 * time.Millisecond) // окно для гонки параллельных доставок
	return nil
}

type nopDLQ struct{}

func (nopDLQ) Publish(context.Context, app.Message, error, int) error { return nil }

type nopMetrics struct{}

func (nopMetrics) Consumed()                   {}
func (nopMetrics) Duplicate()                  {}
func (nopMetrics) Failed()                     {}
func (nopMetrics) DeadLettered()               {}
func (nopMetrics) DeliveryDelay(time.Duration) {}

// Повторная доставка одного события (последовательно и параллельно, как при ребалансе) не создаёт второго
// уведомления: processed_events + уникальный (order_id, kind) в одной транзакции. Требует Docker.
func TestRedeliveryCreatesSingleNotification(t *testing.T) {
	if testing.Short() {
		t.Skip("интеграционный тест: нужен Docker (пропуск с -short)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pgc, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("soldout"), tcpostgres.WithUsername("soldout"), tcpostgres.WithPassword("soldout"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("postgres container: %v", err)
	}
	t.Cleanup(func() { _ = pgc.Terminate(context.Background()) })
	url, err := pgc.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, url, slog.Default()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := db.NewPool(ctx, url, db.PoolOptions{MaxConns: 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	sender := &countingSender{}
	proc := app.New(notificationpg.New(pool), sender, nopDLQ{}, nopMetrics{},
		app.Options{MaxAttempts: 3, Backoff: []time.Duration{10 * time.Millisecond}}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ev := bookingapi.OrderPaidEvent{ID: uuid.New(), OrderID: uuid.New(), EventID: uuid.New(), UserID: uuid.New(), SeatIDs: []int64{1}, AmountMinor: 1, At: time.Now()}
	body, _ := json.Marshal(ev)
	msg := app.Message{Topic: "outbox.event.order", Key: ev.OrderID.String(), Value: body, Headers: map[string]string{"id": ev.ID.String()}}

	if err := proc.Handle(ctx, msg); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ { // повторы, часть — параллельно
		wg.Add(1)
		go func() { defer wg.Done(); _ = proc.Handle(ctx, msg) }()
	}
	wg.Wait()

	var rows, sent, processed int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status = 'sent') FROM notifications WHERE order_id = $1`, ev.OrderID).Scan(&rows, &sent); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifier.processed_events WHERE event_id = $1`, ev.ID).Scan(&processed); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || sent != 1 || processed != 1 {
		t.Fatalf("уведомлений %d (sent %d), processed_events %d: ожидали по одному", rows, sent, processed)
	}
	if n := sender.n.Load(); n != 1 {
		t.Fatalf("письмо отправлено %d раз, ожидали 1", n)
	}
}
