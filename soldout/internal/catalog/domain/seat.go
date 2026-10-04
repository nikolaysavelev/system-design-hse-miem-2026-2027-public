package domain

import (
	"sort"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/catalog/api"
)

// Sector — сектор площадки.
type Sector struct {
	ID       uuid.UUID
	VenueID  uuid.UUID
	Name     string
	Kind     api.SectorKind
	Capacity int
}

// Seat — место.
type Seat struct {
	ID       int64
	SectorID uuid.UUID
	VenueID  uuid.UUID
	RowNo    int
	SeatNo   int
}

// ToAPI — DTO.
func (s Seat) ToAPI() api.Seat {
	return api.Seat{ID: s.ID, SectorID: s.SectorID, VenueID: s.VenueID, RowNo: s.RowNo, SeatNo: s.SeatNo}
}

// BuildSeatMap собирает карту зала: места группируются по секторам, статус берётся из states
// (held/sold), остальные — free. Секторы и места отсортированы детерминированно.
func BuildSeatMap(eventID uuid.UUID, sectors []Sector, seats []Seat, states map[int64]api.SeatStatus) api.SeatMap {
	bySector := make(map[uuid.UUID][]api.SeatMapSeat, len(sectors))
	for _, s := range seats {
		st, ok := states[s.ID]
		if !ok {
			st = api.SeatFree
		}
		bySector[s.SectorID] = append(bySector[s.SectorID], api.SeatMapSeat{ID: s.ID, RowNo: s.RowNo, SeatNo: s.SeatNo, Status: st})
	}
	out := api.SeatMap{EventID: eventID, Sectors: make([]api.SeatMapSector, 0, len(sectors))}
	sorted := append([]Sector(nil), sectors...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, sec := range sorted {
		ss := bySector[sec.ID]
		sort.Slice(ss, func(i, j int) bool { return ss[i].ID < ss[j].ID })
		if ss == nil {
			ss = []api.SeatMapSeat{}
		}
		out.Sectors = append(out.Sectors, api.SeatMapSector{ID: sec.ID, Name: sec.Name, Kind: sec.Kind, Seats: ss})
	}
	return out
}
