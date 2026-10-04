package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/catalog/api"
)

func TestNewEvent_Validation(t *testing.T) {
	now := time.Now()
	venue := uuid.New()
	cases := []struct {
		name string
		in   api.CreateEventInput
		ok   bool
	}{
		{"ok", api.CreateEventInput{Name: "Концерт", VenueID: venue, StartsAt: now.Add(48 * time.Hour), SalesOpenAt: now}, true},
		{"empty name", api.CreateEventInput{Name: " ", VenueID: venue, StartsAt: now.Add(time.Hour), SalesOpenAt: now}, false},
		{"no venue", api.CreateEventInput{Name: "x", StartsAt: now.Add(time.Hour), SalesOpenAt: now}, false},
		{"sales after start", api.CreateEventInput{Name: "x", VenueID: venue, StartsAt: now, SalesOpenAt: now.Add(time.Hour)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, err := NewEvent(c.in)
			if c.ok && err != nil {
				t.Fatalf("ожидался успех, получено %v", err)
			}
			if !c.ok && !errors.Is(err, api.ErrValidation) {
				t.Fatalf("ожидалась ErrValidation, получено %v", err)
			}
			if c.ok && ev.SalesState != api.SalesScheduled {
				t.Fatalf("новое мероприятие должно быть scheduled, получено %s", ev.SalesState)
			}
		})
	}
}

func TestEvent_Open(t *testing.T) {
	ev := Event{SalesState: api.SalesScheduled}
	if err := ev.Open(); err != nil || ev.SalesState != api.SalesOpen {
		t.Fatalf("scheduled → open: err=%v state=%s", err, ev.SalesState)
	}
	if err := ev.Open(); err != nil {
		t.Fatalf("open → open должно быть идемпотентно: %v", err)
	}
	ev.SalesState = api.SalesClosed
	if err := ev.Open(); !errors.Is(err, api.ErrInvalidTransition) {
		t.Fatalf("closed → open должно быть запрещено, получено %v", err)
	}
}

func TestBuildSeatMap(t *testing.T) {
	ev := uuid.New()
	secA, secB := uuid.New(), uuid.New()
	sectors := []Sector{{ID: secB, Name: "Сектор 2", Kind: api.SectorSeated}, {ID: secA, Name: "Сектор 1", Kind: api.SectorSeated}}
	seats := []Seat{{ID: 3, SectorID: secB, RowNo: 1, SeatNo: 1}, {ID: 2, SectorID: secA, RowNo: 1, SeatNo: 2}, {ID: 1, SectorID: secA, RowNo: 1, SeatNo: 1}}
	sm := BuildSeatMap(ev, sectors, seats, map[int64]api.SeatStatus{2: api.SeatHeld, 3: api.SeatSold})

	if len(sm.Sectors) != 2 || sm.Sectors[0].Name != "Сектор 1" {
		t.Fatalf("секторы должны быть отсортированы по имени: %+v", sm.Sectors)
	}
	got := sm.Sectors[0].Seats
	if len(got) != 2 || got[0].ID != 1 || got[0].Status != api.SeatFree || got[1].Status != api.SeatHeld {
		t.Fatalf("неверные статусы/порядок мест сектора 1: %+v", got)
	}
	if sm.Sectors[1].Seats[0].Status != api.SeatSold {
		t.Fatalf("место 3 должно быть sold: %+v", sm.Sectors[1].Seats)
	}
}
