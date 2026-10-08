package domain

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/booking/api"
)

// Групповой hold (HW1, V3): от 2 до 4 смежных мест одного ряда.
const (
	MinGroupSize = 2
	MaxGroupSize = 4
)

// SeatPos — место сектора: ряд и номер в ряду.
type SeatPos struct {
	ID    int64
	RowNo int
	No    int
}

// Group — запрос на N смежных мест.
type Group struct {
	ID             uuid.UUID
	EventID        uuid.UUID
	SectorID       uuid.UUID
	UserID         uuid.UUID
	N              int
	IdempotencyKey string
	CreatedAt      time.Time
}

// ValidateGroupSize — N от 2 до 4.
func ValidateGroupSize(n int) error {
	if n < MinGroupSize || n > MaxGroupSize {
		return fmt.Errorf("%w: n от %d до %d, получено %d", api.ErrValidation, MinGroupSize, MaxGroupSize, n)
	}
	return nil
}

// CanHoldGroup — FR-7 для группы: у пользователя current живых hold'ов, группа добавляет n.
func CanHoldGroup(current, n int) bool { return current+n <= api.MaxTicketsPerUser }

// FindAdjacent ищет первый свободный отрезок из n мест одного ряда: ряды с preferRow по возрастанию, затем ряды
// до preferRow; в ряду — слева направо. preferRow <= 0 — с первого ряда. Ряд 0 (танцпол) не рассматривается.
// Возвращает id мест отрезка по возрастанию номера или nil.
func FindAdjacent(seats []SeatPos, taken map[int64]bool, n, preferRow int) []int64 {
	rows := map[int][]SeatPos{}
	for _, s := range seats {
		if s.RowNo > 0 {
			rows[s.RowNo] = append(rows[s.RowNo], s)
		}
	}
	order := make([]int, 0, len(rows))
	for r := range rows {
		order = append(order, r)
	}
	sort.Slice(order, func(i, j int) bool {
		ai, aj := order[i] >= preferRow, order[j] >= preferRow
		if ai != aj {
			return ai
		}
		return order[i] < order[j]
	})
	for _, r := range order {
		row := rows[r]
		sort.Slice(row, func(i, j int) bool { return row[i].No < row[j].No })
		run := 0
		for i, s := range row {
			switch {
			case taken[s.ID]:
				run = 0
				continue
			case run > 0 && row[i-1].No == s.No-1:
				run++
			default:
				run = 1
			}
			if run == n {
				out := make([]int64, 0, n)
				for _, p := range row[i-n+1 : i+1] {
					out = append(out, p.ID)
				}
				return out
			}
		}
	}
	return nil
}

// NewGroupHolds — active hold'ы группы на места seatIDs с общим group_id и сроком.
func NewGroupHolds(g Group, seatIDs []int64, now time.Time, ttl time.Duration) []Hold {
	out := make([]Hold, 0, len(seatIDs))
	for _, seatID := range seatIDs {
		h := NewHold(g.EventID, seatID, g.UserID, now, ttl)
		gid := g.ID
		h.GroupID = &gid
		out = append(out, h)
	}
	return out
}
