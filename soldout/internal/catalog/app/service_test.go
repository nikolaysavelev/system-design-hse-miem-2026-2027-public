package app

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/catalog/api"
	"github.com/nikolaysavelev/soldout/internal/catalog/domain"
)

// fakeRepo считает обращения к «БД» за картой сектора.
type fakeRepo struct {
	Repository
	event  domain.Event
	sector domain.Sector
	calls  atomic.Int64
	delay  time.Duration
}

func (f *fakeRepo) GetEvent(context.Context, uuid.UUID) (domain.Event, error)   { return f.event, nil }
func (f *fakeRepo) GetSector(context.Context, uuid.UUID) (domain.Sector, error) { return f.sector, nil }
func (f *fakeRepo) SeatsBySector(context.Context, uuid.UUID) ([]domain.Seat, error) {
	return []domain.Seat{{ID: 1, SectorID: f.sector.ID, RowNo: 1, SeatNo: 1}}, nil
}
func (f *fakeRepo) SectorStates(context.Context, uuid.UUID, uuid.UUID) ([]domain.SeatState, int64, error) {
	f.calls.Add(1)
	time.Sleep(f.delay)
	return nil, 3, nil
}

type fakeCache struct {
	mu      sync.Mutex
	payload []byte
	version int64
	want    int64
	sets    int
}

func (c *fakeCache) Get(context.Context, uuid.UUID, uuid.UUID) ([]byte, int64, bool, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.payload, c.version, c.payload != nil, c.want > c.version, nil
}
func (c *fakeCache) Set(_ context.Context, _, _ uuid.UUID, p []byte, v int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v >= c.version {
		c.payload, c.version = p, v
	}
	c.sets++
	return nil
}
func (c *fakeCache) Invalidate(_ context.Context, _, _ uuid.UUID, v int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v > c.want {
		c.want = v
	}
	return nil
}

// 100 параллельных промахов → один запрос к БД (singleflight), дальше — попадания.
func TestSectorSeatMap_SingleflightOnMiss(t *testing.T) {
	ev, sec := uuid.New(), uuid.New()
	venue := uuid.New()
	repo := &fakeRepo{event: domain.Event{ID: ev, VenueID: venue}, sector: domain.Sector{ID: sec, VenueID: venue, Name: "S", Kind: api.SectorSeated, Capacity: 1}, delay: 20 * time.Millisecond}
	cache := &fakeCache{}
	svc := New(repo, cache, Options{CacheTTL: time.Minute, CacheInvalidate: true}, nil, slog.Default())

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.SectorSeatMap(context.Background(), ev, sec)
			if err != nil || res.Version != 3 || len(res.JSON) == 0 {
				t.Errorf("неверный ответ: %+v %v", res, err)
			}
		}()
	}
	wg.Wait()
	if n := repo.calls.Load(); n != 1 {
		t.Fatalf("ожидался 1 запрос к БД, получено %d", n)
	}
	res, _ := svc.SectorSeatMap(context.Background(), ev, sec)
	if !res.Cached {
		t.Fatal("после прогрева ожидалось попадание в кэш")
	}
	// инвалидация помечает данные stale: читатель получает их сразу (Stale=true), обновление — фоном
	_ = cache.Invalidate(context.Background(), ev, sec, 4)
	res, _ = svc.SectorSeatMap(context.Background(), ev, sec)
	if !res.Stale || res.Version != 3 {
		t.Fatalf("после инвалидации ожидались stale-данные версии 3: %+v", res)
	}
	time.Sleep(60 * time.Millisecond) // фоновое обновление: rebuild → Set(v=3) — версия из «БД» не изменилась, CAS принимает
	if n := repo.calls.Load(); n != 2 {
		t.Fatalf("ожидалось одно фоновое обновление (2 запроса к БД всего), получено %d", n)
	}
}
