package outbox_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	gootel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/nikolaysavelev/soldout/internal/platform/db"
	"github.com/nikolaysavelev/soldout/internal/platform/otel"
	"github.com/nikolaysavelev/soldout/internal/platform/outbox"
)

type testEvent struct {
	ID      uuid.UUID `json:"id"`
	OrderID uuid.UUID `json:"order_id"`
}

func (e testEvent) OutboxID() uuid.UUID   { return e.ID }
func (e testEvent) AggregateType() string { return "order" }
func (e testEvent) AggregateID() string   { return e.OrderID.String() }
func (e testEvent) EventType() string     { return "OrderPaid" }

func startDB(t *testing.T) *db.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("интеграционный тест: нужен Docker (пропуск с -short)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
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
	pool, err := db.NewPool(ctx, url, db.PoolOptions{MaxConns: 5}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func count(t *testing.T, pool *db.Pool, id uuid.UUID) (n int, traceparent string) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `SELECT count(*), coalesce(max(traceparent), '') FROM outbox WHERE id = $1`, id).Scan(&n, &traceparent)
	if err != nil {
		t.Fatal(err)
	}
	return n, traceparent
}

// Строка outbox пишется в транзакции издателя: rollback отменяет событие вместе с изменением,
// commit делает его видимым; traceparent заполнен, если в ctx есть span. Режим CDC: таблица пуста после commit.
func TestOutboxAppend(t *testing.T) {
	pool := startDB(t)
	ctx := context.Background()

	tp := sdktrace.NewTracerProvider()
	gootel.SetTracerProvider(tp)
	gootel.SetTextMapPropagator(propagation.TraceContext{})
	spanCtx, span := otel.Start(ctx, "booking.PayOrder")
	defer span.End()

	keep := outbox.NewWriter(outbox.Options{DeleteAfterInsert: false})
	errBoom := errors.New("rollback")

	t.Run("rollback отменяет событие", func(t *testing.T) {
		e := testEvent{ID: uuid.New(), OrderID: uuid.New()}
		err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
			if err := keep.Append(spanCtx, tx, e); err != nil {
				return err
			}
			return errBoom
		})
		if !errors.Is(err, errBoom) {
			t.Fatalf("ожидали ошибку транзакции, получили %v", err)
		}
		if n, _ := count(t, pool, e.ID); n != 0 {
			t.Fatalf("после rollback строка outbox видна: %d", n)
		}
	})

	t.Run("commit публикует, traceparent из span", func(t *testing.T) {
		e := testEvent{ID: uuid.New(), OrderID: uuid.New()}
		if err := db.WithTx(ctx, pool, func(tx pgx.Tx) error { return keep.Append(spanCtx, tx, e) }); err != nil {
			t.Fatal(err)
		}
		n, traceparent := count(t, pool, e.ID)
		if n != 1 {
			t.Fatalf("после commit ожидали 1 строку, получили %d", n)
		}
		if want := "00-" + span.SpanContext().TraceID().String(); len(traceparent) < len(want) || traceparent[:len(want)] != want {
			t.Fatalf("traceparent %q не из текущего трейса %s", traceparent, span.SpanContext().TraceID())
		}
	})

	t.Run("без span traceparent пуст", func(t *testing.T) {
		e := testEvent{ID: uuid.New(), OrderID: uuid.New()}
		if err := db.WithTx(ctx, pool, func(tx pgx.Tx) error { return keep.Append(ctx, tx, e) }); err != nil {
			t.Fatal(err)
		}
		if _, traceparent := count(t, pool, e.ID); traceparent != "" {
			t.Fatalf("traceparent без span: %q", traceparent)
		}
	})

	t.Run("CDC: строка удаляется в той же транзакции", func(t *testing.T) {
		e := testEvent{ID: uuid.New(), OrderID: uuid.New()}
		cdc := outbox.NewWriter(outbox.Options{DeleteAfterInsert: true})
		if err := db.WithTx(ctx, pool, func(tx pgx.Tx) error { return cdc.Append(spanCtx, tx, e) }); err != nil {
			t.Fatal(err)
		}
		if n, _ := count(t, pool, e.ID); n != 0 {
			t.Fatalf("в режиме CDC таблица должна быть пуста, строк: %d", n)
		}
	})

	t.Run("poller отдаёт и удаляет строки", func(t *testing.T) {
		poller := outbox.NewPoller(pool, 100, 10*time.Millisecond)
		// отмена посреди пачки не даёт ack: пачка придёт снова (at-least-once), поэтому здесь просто таймаут
		pctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		var got int
		_ = poller.Run(pctx, func(_ context.Context, batch []outbox.Record) error {
			got += len(batch)
			return nil
		})
		if got != 2 { // две строки из подтестов без удаления
			t.Fatalf("poller отдал %d строк, ожидали 2", got)
		}
		var left int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left != 0 {
			t.Fatalf("после ack в outbox осталось %d строк", left)
		}
	})
}
