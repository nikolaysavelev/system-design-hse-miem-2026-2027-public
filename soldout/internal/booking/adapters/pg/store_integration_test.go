package pg_test

import (
	"context"
	"errors"
	"fmt"
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
	catalogapi "github.com/nikolaysavelev/soldout/internal/catalog/api"
	"github.com/nikolaysavelev/soldout/internal/platform/db"
	queueapi "github.com/nikolaysavelev/soldout/internal/queue/api"
)

// Интеграционный тест: два (и более) конкурентных hold на одно место → ровно один успех (I1).
// Требует Docker; пропускается с -short.
func TestConcurrentHolds_ExactlyOneWins(t *testing.T) {
	if testing.Short() {
		t.Skip("интеграционный тест: нужен Docker (пропуск с -short)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := startPostgres(ctx, t)

	eventID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	venueID := uuid.MustParse("20000000-0000-4000-8000-000000000001")
	svc := app.New(bookingpg.New(pool), fakeCatalog{eventID: eventID, venueID: venueID}, fakeQueue{}, fakePayment{}, fakeTickets{},
		app.Options{HoldTTL: time.Minute, TicketPriceMinor: 100}, slog.Default())

	const workers = 20
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.CreateHold(ctx, api.CreateHoldInput{EventID: eventID, SeatID: 1, UserID: uuid.New(), AdmissionToken: "ok"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var ok, held int
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, api.ErrSeatHeld):
			held++
		default:
			t.Errorf("неожиданная ошибка: %v", err)
		}
	}
	if ok != 1 || held != workers-1 {
		t.Fatalf("ожидался ровно один успех и %d конфликтов, получено ok=%d held=%d", workers-1, ok, held)
	}

	// Лимит 4: пятый hold того же пользователя на другое место → ErrHoldLimit.
	user := uuid.New()
	for seat := int64(10); seat < 14; seat++ {
		if _, err := svc.CreateHold(ctx, api.CreateHoldInput{EventID: eventID, SeatID: seat, UserID: user, AdmissionToken: "ok"}); err != nil {
			t.Fatalf("hold %d: %v", seat, err)
		}
	}
	if _, err := svc.CreateHold(ctx, api.CreateHoldInput{EventID: eventID, SeatID: 14, UserID: user, AdmissionToken: "ok"}); !errors.Is(err, api.ErrHoldLimit) {
		t.Fatalf("пятый hold должен упереться в лимит, получено %v", err)
	}

	// Освобождаем один hold → лимит снова позволяет; полный цикл order → pay → sold, повтор pay идемпотентен.
	orders0, err := bookingpg.New(pool).ListUserOrders(ctx, user)
	if err != nil || len(orders0) != 0 {
		t.Fatalf("у пользователя не должно быть заказов: %v %d", err, len(orders0))
	}
	first, err := svc.SeatStates(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}
	if first[10] != api.SeatStateHeld {
		t.Fatalf("место 10 должно быть held: %v", first[10])
	}
	var holdID uuid.UUID
	// находим id hold на место 13 через прямой запрос
	if err := pool.QueryRow(ctx, "/* test */ SELECT id FROM holds WHERE seat_id = 13 AND status = 'active'").Scan(&holdID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReleaseHold(ctx, holdID); err != nil {
		t.Fatalf("release: %v", err)
	}
	h, err := svc.CreateHold(ctx, api.CreateHoldInput{EventID: eventID, SeatID: 100, UserID: user, AdmissionToken: "ok"})
	if err != nil {
		t.Fatalf("hold после освобождения: %v", err)
	}
	order, created, err := svc.CreateOrder(ctx, api.CreateOrderInput{HoldIDs: []uuid.UUID{h.ID}, UserID: user, IdempotencyKey: "k-1"})
	if err != nil || !created || order.Status != api.OrderPending || order.AmountMinor != 100 {
		t.Fatalf("create order: %+v created=%v err=%v", order, created, err)
	}
	if again, created2, err := svc.CreateOrder(ctx, api.CreateOrderInput{HoldIDs: []uuid.UUID{h.ID}, UserID: user, IdempotencyKey: "k-1"}); err != nil || created2 || again.ID != order.ID {
		t.Fatalf("повтор с тем же Idempotency-Key должен вернуть тот же заказ: %+v created=%v err=%v", again, created2, err)
	}
	if _, _, err := svc.CreateOrder(ctx, api.CreateOrderInput{HoldIDs: []uuid.UUID{h.ID}, UserID: user, IdempotencyKey: "k-2"}); !errors.Is(err, api.ErrHoldAlreadyOrdered) {
		t.Fatalf("hold в двух заказах запрещён: %v", err)
	}
	paid, err := svc.PayOrder(ctx, api.PayOrderInput{OrderID: order.ID})
	if err != nil || paid.Status != api.OrderPaid {
		t.Fatalf("pay: %+v %v", paid, err)
	}
	if again, err := svc.PayOrder(ctx, api.PayOrderInput{OrderID: order.ID}); err != nil || again.Status != api.OrderPaid {
		t.Fatalf("повторный pay должен быть идемпотентен: %+v %v", again, err)
	}
	states, err := svc.SeatStates(ctx, eventID)
	if err != nil || states[100] != api.SeatStateSold {
		t.Fatalf("место 100 должно быть sold: %v %v", states[100], err)
	}
	if n, err := svc.SoldCount(ctx, eventID); err != nil || n != 1 {
		t.Fatalf("SoldCount = %d, %v; ожидалось 1", n, err)
	}
	if _, err := svc.CreateHold(ctx, api.CreateHoldInput{EventID: eventID, SeatID: 100, UserID: uuid.New(), AdmissionToken: "ok"}); !errors.Is(err, api.ErrSeatSold) {
		t.Fatalf("проданное место нельзя удержать: %v", err)
	}
	// Отсутствие токена допуска → ErrAdmissionRequired.
	if _, err := svc.CreateHold(ctx, api.CreateHoldInput{EventID: eventID, SeatID: 101, UserID: uuid.New()}); !errors.Is(err, api.ErrAdmissionRequired) {
		t.Fatalf("без токена ожидался ErrAdmissionRequired: %v", err)
	}
}

type fakeCatalog struct{ eventID, venueID uuid.UUID }

func (f fakeCatalog) ListEvents(context.Context) ([]catalogapi.Event, error) { return nil, nil }
func (f fakeCatalog) GetEvent(_ context.Context, id uuid.UUID) (catalogapi.Event, error) {
	if id != f.eventID {
		return catalogapi.Event{}, catalogapi.ErrEventNotFound
	}
	return catalogapi.Event{ID: id, VenueID: f.venueID, SalesState: catalogapi.SalesOpen}, nil
}
func (f fakeCatalog) GetSeat(_ context.Context, seatID int64) (catalogapi.Seat, error) {
	return catalogapi.Seat{ID: seatID, VenueID: f.venueID}, nil
}
func (f fakeCatalog) SeatMap(context.Context, uuid.UUID) (catalogapi.SeatMap, error) {
	return catalogapi.SeatMap{}, nil
}
func (f fakeCatalog) SectorSeatMap(context.Context, uuid.UUID, uuid.UUID) (catalogapi.SectorSeatMapResult, error) {
	return catalogapi.SectorSeatMapResult{}, nil
}
func (f fakeCatalog) CreateEvent(context.Context, catalogapi.CreateEventInput) (catalogapi.Event, error) {
	return catalogapi.Event{}, nil
}
func (f fakeCatalog) OpenSales(context.Context, uuid.UUID) (catalogapi.Event, error) {
	return catalogapi.Event{}, nil
}

// startPostgres поднимает PostgreSQL 17 в контейнере, применяет миграции и seed (3 × 3 000 мест).
func startPostgres(ctx context.Context, t *testing.T) *db.Pool {
	t.Helper()
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
	pool, err := db.NewPool(ctx, url, db.PoolOptions{MaxConns: 50}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Seed(ctx, pool, 3000, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return pool
}

// V3: конкурентные групповые hold'ы в одном секторе получают непересекающиеся отрезки одного ряда;
// при неудаче не удерживается ничего; лимит 4 учитывает всю группу; группа снимается, оплачивается и истекает целиком.
func TestGroupHold_AdjacentSegments(t *testing.T) {
	if testing.Short() {
		t.Skip("интеграционный тест: нужен Docker (пропуск с -short)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := startPostgres(ctx, t)

	eventID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	venueID := uuid.MustParse("20000000-0000-4000-8000-000000000001")
	sectorID := uuid.MustParse("30000000-0000-4000-8000-0000000003e9") // сектор 1: места 1..1000, 20 рядов по 50
	store := bookingpg.New(pool)
	svc := app.New(store, fakeCatalog{eventID: eventID, venueID: venueID}, fakeQueue{}, fakePayment{}, fakeTickets{},
		app.Options{HoldTTL: time.Minute, TicketPriceMinor: 100}, slog.Default())
	in := func(n int, key string) api.CreateGroupHoldInput {
		return api.CreateGroupHoldInput{EventID: eventID, SectorID: sectorID, UserID: uuid.New(), N: n, PreferRow: 1,
			AdmissionToken: "ok", IdempotencyKey: key}
	}

	// 40 конкурентов одновременно хотят первый ряд; допустим отказ ErrNoAdjacentSeats. Удержанные отрезки
	// не пересекаются, у получивших отказ не осталось ни одного hold'а.
	const workers = 40
	type result struct {
		g    api.GroupHold
		user uuid.UUID
		err  error
	}
	var wg sync.WaitGroup
	results := make(chan result, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := in(2+i%3, fmt.Sprintf("g-%d", i))
			g, _, err := svc.CreateGroupHold(ctx, req)
			results <- result{g: g, user: req.UserID, err: err}
		}(i)
	}
	wg.Wait()
	close(results)
	used := map[int64]bool{}
	won := 0
	for r := range results {
		if r.err != nil {
			if !errors.Is(r.err, api.ErrNoAdjacentSeats) {
				t.Fatalf("неожиданная ошибка: %v", r.err)
			}
			var n int
			if err := pool.QueryRow(ctx, "/* test */ SELECT count(*) FROM holds WHERE user_id = $1", r.user).Scan(&n); err != nil || n != 0 {
				t.Fatalf("после неудачи у пользователя %d hold'ов: %v", n, err)
			}
			continue
		}
		won++
		g := r.g
		seats := make([]int64, 0, len(g.Holds))
		for _, h := range g.Holds {
			if used[h.SeatID] {
				t.Fatalf("место %d удержано двумя группами", h.SeatID)
			}
			used[h.SeatID] = true
			seats = append(seats, h.SeatID)
		}
		row := (seats[0]-1)/50 + 1
		for i, id := range seats {
			if (id-1)/50+1 != row || (i > 0 && id != seats[i-1]+1) {
				t.Fatalf("места группы не отрезок одного ряда: %v", seats)
			}
		}
		if int64(g.RowNo) != row {
			t.Fatalf("ряд в ответе %d, места в ряду %d", g.RowNo, row)
		}
	}
	if won == 0 {
		t.Fatal("ни одна группа не удержана")
	}
	var grouped int
	if err := pool.QueryRow(ctx, "/* test */ SELECT count(*) FROM holds WHERE group_id IS NOT NULL").Scan(&grouped); err != nil || grouped != len(used) {
		t.Fatalf("в БД %d hold'ов групп, у победителей %d мест: %v", grouped, len(used), err)
	}

	// повтор с тем же ключом — та же группа; тот же ключ с другими параметрами — конфликт
	first := in(2, "repeat")
	g1, created, err := svc.CreateGroupHold(ctx, first)
	if err != nil || !created {
		t.Fatalf("группа: %v %v", created, err)
	}
	g2, created, err := svc.CreateGroupHold(ctx, first)
	if err != nil || created || g2.ID != g1.ID || len(g2.Holds) != 2 {
		t.Fatalf("повтор должен вернуть ту же группу: %+v %v %v", g2, created, err)
	}
	other := first
	other.N = 3
	if _, _, err := svc.CreateGroupHold(ctx, other); !errors.Is(err, api.ErrIdempotencyConflict) {
		t.Fatalf("ключ с другими параметрами: %v", err)
	}

	// лимит 4: группа из 3 и ещё группа из 2 у того же пользователя
	limited := in(3, "limit-1")
	if _, _, err := svc.CreateGroupHold(ctx, limited); err != nil {
		t.Fatal(err)
	}
	limited.N, limited.IdempotencyKey = 2, "limit-2"
	if _, _, err := svc.CreateGroupHold(ctx, limited); !errors.Is(err, api.ErrHoldLimit) {
		t.Fatalf("лимит должен учитывать всю группу: %v", err)
	}

	// одиночное снятие члена группы и заказ на часть группы запрещены; заказ по group_id и оплата — вся группа
	buyer := in(3, "buy")
	bg, _, err := svc.CreateGroupHold(ctx, buyer)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReleaseHold(ctx, bg.Holds[0].ID); !errors.Is(err, api.ErrGroupPartial) {
		t.Fatalf("снятие одного hold'а группы: %v", err)
	}
	if _, _, err := svc.CreateOrder(ctx, api.CreateOrderInput{HoldIDs: []uuid.UUID{bg.Holds[0].ID}, UserID: buyer.UserID, IdempotencyKey: "part"}); !errors.Is(err, api.ErrGroupPartial) {
		t.Fatalf("заказ на часть группы: %v", err)
	}
	order, _, err := svc.CreateOrder(ctx, api.CreateOrderInput{GroupID: bg.ID, UserID: buyer.UserID, IdempotencyKey: "whole"})
	if err != nil || len(order.HoldIDs) != 3 || order.AmountMinor != 300 {
		t.Fatalf("заказ на группу: %+v %v", order, err)
	}
	if paid, err := svc.PayOrder(ctx, api.PayOrderInput{OrderID: order.ID}); err != nil || paid.Status != api.OrderPaid {
		t.Fatalf("оплата группы: %+v %v", paid, err)
	}

	// DELETE группы снимает все её hold'ы
	if err := svc.ReleaseGroup(ctx, g1.ID); err != nil {
		t.Fatal(err)
	}
	var active int
	if err := pool.QueryRow(ctx, "/* test */ SELECT count(*) FROM holds WHERE group_id = $1 AND status = 'active'", g1.ID).Scan(&active); err != nil || active != 0 {
		t.Fatalf("после снятия группы active=%d %v", active, err)
	}

	// expirer с батчем 1 снимает группу целиком
	exp := in(4, "expire")
	eg, _, err := svc.CreateGroupHold(ctx, exp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "/* test */ UPDATE holds SET expires_at = now() - interval '1 hour' WHERE group_id = $1", eg.ID); err != nil {
		t.Fatal(err)
	}
	released, err := store.ExpireHoldsBatch(ctx, time.Now(), 1)
	if err != nil || len(released) != 4 {
		t.Fatalf("expirer должен снять группу целиком: %d %v", len(released), err)
	}

	// все нечётные места свободны, все чётные заняты — смежной пары нет: 409 и ни одного нового hold'а
	if _, err := pool.Exec(ctx, `/* test */ INSERT INTO holds (id, event_id, seat_id, user_id, status, expires_at)
		SELECT gen_random_uuid(), $1, id, gen_random_uuid(), 'active', now() + interval '1 hour'
		FROM seats WHERE sector_id = $2 AND seat_no % 2 = 0 ON CONFLICT DO NOTHING`, eventID, sectorID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `/* test */ UPDATE holds SET status = 'released'
		WHERE status = 'active' AND seat_id IN (SELECT id FROM seats WHERE sector_id = $1 AND seat_no % 2 = 1)`, sectorID); err != nil {
		t.Fatal(err)
	}
	none := in(2, "none")
	if _, _, err := svc.CreateGroupHold(ctx, none); !errors.Is(err, api.ErrNoAdjacentSeats) {
		t.Fatalf("ожидался ErrNoAdjacentSeats: %v", err)
	}
	var mine int
	if err := pool.QueryRow(ctx, "/* test */ SELECT count(*) FROM holds WHERE user_id = $1", none.UserID).Scan(&mine); err != nil || mine != 0 {
		t.Fatalf("при неудаче не должно остаться hold'ов: %d %v", mine, err)
	}
}

type fakeQueue struct{}

func (fakeQueue) Join(context.Context, uuid.UUID, uuid.UUID) (queueapi.Admission, error) {
	return queueapi.Admission{Token: "ok"}, nil
}
func (fakeQueue) Validate(_ context.Context, token string, _, _ uuid.UUID) error {
	if token != "ok" {
		return queueapi.ErrAdmissionInvalid
	}
	return nil
}

type fakePayment struct{}

func (fakePayment) Charge(context.Context, api.ChargeRequest) (api.ChargeResult, error) {
	return api.ChargeResult{Succeeded: true, PSPRef: "test"}, nil
}

type fakeTickets struct{}

func (fakeTickets) Issue(context.Context, api.IssueRequest) error { return nil }
