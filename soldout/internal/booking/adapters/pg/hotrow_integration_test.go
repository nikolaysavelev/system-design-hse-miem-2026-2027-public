package pg_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	bookingpg "github.com/nikolaysavelev/soldout/internal/booking/adapters/pg"
	"github.com/nikolaysavelev/soldout/internal/booking/api"
	"github.com/nikolaysavelev/soldout/internal/booking/app"
	"github.com/nikolaysavelev/soldout/internal/platform/db"
)

// Тесты шага C (занятие 2): детерминизм через инварианты, не через тайминги. Один контейнер PostgreSQL на тест,
// fsync=off, max_connections=200, MaxConns = concurrency + 2.
func startPG(t *testing.T, ctx context.Context, maxConns int32) *db.Pool {
	t.Helper()
	pgc, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("soldout"), tcpostgres.WithUsername("soldout"), tcpostgres.WithPassword("soldout"),
		testcontainers.WithCmdArgs("-c", "fsync=off", "-c", "max_connections=200"),
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
	pool, err := db.NewPool(ctx, url, db.PoolOptions{MaxConns: maxConns, AcquireTimeout: 10 * time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Seed(ctx, pool, 3000, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return pool
}

var (
	testEvent = uuid.MustParse("10000000-0000-4000-8000-000000000001")
	testVenue = uuid.MustParse("20000000-0000-4000-8000-000000000001")
)

// 20 конкурентов стартуют по close(start) на одно место → ровно 1 успех, 19 ErrSeatHeld, 0 DeadlineExceeded.
func TestHotRow_SpecificSeat_ExactlyOneWins(t *testing.T) {
	if testing.Short() {
		t.Skip("интеграционный тест: нужен Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const n = 20
	pool := startPG(t, ctx, n+2)
	svc := app.New(bookingpg.New(pool), fakeCatalog{eventID: testEvent, venueID: testVenue}, fakeQueue{}, fakePayment{}, fakeTickets{},
		app.Options{HoldTTL: time.Minute, TicketPriceMinor: 100}, slog.Default())

	start := make(chan struct{})
	results := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.CreateHold(ctx, api.CreateHoldInput{EventID: testEvent, SeatID: 7, UserID: uuid.New(), AdmissionToken: "ok"})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var ok, held, deadline int
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, api.ErrSeatHeld):
			held++
		case errors.Is(err, context.DeadlineExceeded):
			deadline++
		default:
			t.Errorf("неожиданная ошибка: %v", err)
		}
	}
	if ok != 1 || held != n-1 || deadline != 0 {
		t.Fatalf("ожидалось ok=1 held=%d deadline=0, получено ok=%d held=%d deadline=%d", n-1, ok, held, deadline)
	}
}

// 50 конкурентов POST /holds/any на сектор с 20 свободными местами → ровно 20 успехов, 20 разных мест, 30 sector_exhausted.
func TestHotRow_AnySeat_ExactlyFreeSeatsWin(t *testing.T) {
	if testing.Short() {
		t.Skip("интеграционный тест: нужен Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const n, free = 50, 20
	pool := startPG(t, ctx, n+2)
	sector := uuid.MustParse("30000000-0000-4000-8000-0000000003e9") // сектор 1 мероприятия 1: места 1..1000
	// оставляем в «секторе» 20 мест: catalog-заглушка отдаёт кандидатами только seat_id 101..120
	seats := make([]int64, 0, free)
	for i := int64(101); i <= 120; i++ {
		seats = append(seats, i)
	}
	cat := fakeCatalog{eventID: testEvent, venueID: testVenue, sectorSeats: map[uuid.UUID][]int64{sector: seats}}
	svc := app.New(bookingpg.New(pool), cat, fakeQueue{}, fakePayment{}, fakeTickets{},
		app.Options{HoldTTL: time.Minute, TicketPriceMinor: 100}, slog.Default())

	start := make(chan struct{})
	type res struct {
		seat int64
		err  error
	}
	results := make(chan res, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			h, _, err := svc.CreateAnyHold(ctx, api.CreateAnyHoldInput{EventID: testEvent, SectorID: sector, UserID: uuid.New(), AdmissionToken: "ok", IdempotencyKey: uuid.NewString(), RequestHash: "h"})
			results <- res{seat: h.SeatID, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	won := map[int64]bool{}
	var exhausted int
	for r := range results {
		switch {
		case r.err == nil:
			if won[r.seat] {
				t.Fatalf("место %d выдано дважды", r.seat)
			}
			won[r.seat] = true
		case errors.Is(r.err, api.ErrSectorExhausted):
			exhausted++
		default:
			t.Errorf("неожиданная ошибка: %v", r.err)
		}
	}
	if len(won) != free || exhausted != n-free {
		t.Fatalf("ожидалось %d успехов и %d sector_exhausted, получено %d и %d", free, n-free, len(won), exhausted)
	}
}

// 10 ретраев с одним Idempotency-Key → ровно 1 hold; другое тело с тем же ключом → ErrIdempotencyConflict.
func TestHotRow_AnySeat_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("интеграционный тест: нужен Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const n = 10
	pool := startPG(t, ctx, n+2)
	sector := uuid.MustParse("30000000-0000-4000-8000-0000000003e9")
	cat := fakeCatalog{eventID: testEvent, venueID: testVenue, sectorSeats: map[uuid.UUID][]int64{sector: {201, 202, 203, 204, 205}}}
	svc := app.New(bookingpg.New(pool), cat, fakeQueue{}, fakePayment{}, fakeTickets{},
		app.Options{HoldTTL: time.Minute, TicketPriceMinor: 100}, slog.Default())
	user, key := uuid.New(), uuid.NewString()

	start := make(chan struct{})
	results := make(chan api.Hold, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				h, _, err := svc.CreateAnyHold(ctx, api.CreateAnyHoldInput{EventID: testEvent, SectorID: sector, UserID: user, AdmissionToken: "ok", IdempotencyKey: key, RequestHash: "h"})
				if errors.Is(err, api.ErrIdempotencyInFlight) {
					time.Sleep(5 * time.Millisecond)
					continue
				}
				if err != nil {
					t.Errorf("ретрай: %v", err)
				}
				results <- h
				return
			}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	ids := map[uuid.UUID]bool{}
	for h := range results {
		ids[h.ID] = true
	}
	if len(ids) != 1 {
		t.Fatalf("ожидался ровно один hold, получено %d", len(ids))
	}
	var holds int
	if err := pool.QueryRow(ctx, "/* test */ SELECT count(*) FROM holds WHERE user_id = $1", user).Scan(&holds); err != nil || holds != 1 {
		t.Fatalf("в БД должен быть один hold, получено %d (%v)", holds, err)
	}
	if _, _, err := svc.CreateAnyHold(ctx, api.CreateAnyHoldInput{EventID: testEvent, SectorID: sector, UserID: user, AdmissionToken: "ok", IdempotencyKey: key, RequestHash: "другое-тело"}); !errors.Is(err, api.ErrIdempotencyConflict) {
		t.Fatalf("другое тело с тем же ключом → ErrIdempotencyConflict, получено %v", err)
	}
}
