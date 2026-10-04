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

func TestBuildSectorSeatMap_AndSummary(t *testing.T) {
	ev := uuid.New()
	sec := Sector{ID: uuid.New(), Name: "Сектор 1", Kind: api.SectorSeated, Capacity: 3}
	seats := []Seat{{ID: 3, SectorID: sec.ID, RowNo: 1, SeatNo: 3}, {ID: 1, SectorID: sec.ID, RowNo: 1, SeatNo: 1}, {ID: 2, SectorID: sec.ID, RowNo: 1, SeatNo: 2}}
	sm := BuildSectorSeatMap(ev, sec, seats, []SeatState{{SeatID: 2, Status: api.SeatHeld}, {SeatID: 3, Status: api.SeatSold}}, 7)
	if sm.Version != 7 || len(sm.Seats) != 3 || sm.Seats[0].ID != 1 || sm.Seats[0].Status != api.SeatFree || sm.Seats[1].Status != api.SeatHeld || sm.Seats[2].Status != api.SeatSold {
		t.Fatalf("неверная карта сектора: %+v", sm)
	}
	sum := BuildSummary(ev, []Sector{sec}, map[uuid.UUID]SectorCounts{sec.ID: {Held: 1, Sold: 1, Version: 7}})
	if s := sum.Sectors[0]; s.Total != 3 || s.Free != 1 || s.Held != 1 || s.Sold != 1 || s.Version != 7 {
		t.Fatalf("неверная сводка: %+v", s)
	}
}
