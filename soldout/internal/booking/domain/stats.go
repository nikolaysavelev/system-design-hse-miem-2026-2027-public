package domain

import "github.com/nikolaysavelev/soldout/internal/booking/api"

// SoldSeats считает проданные места по карте статусов booking.
func SoldSeats(states map[int64]api.SeatState) int {
	n := 0
	for _, st := range states {
		if st == api.SeatStateSold {
			n++
		}
	}
	return n
}
