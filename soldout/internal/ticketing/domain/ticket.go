// Package domain — билет с уникальным кодом.
package domain

import (
	"crypto/rand"
	"encoding/base32"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/ticketing/api"
)

// Ticket — билет.
type Ticket struct {
	ID       uuid.UUID
	OrderID  uuid.UUID
	EventID  uuid.UUID
	SeatID   int64
	Code     string
	IssuedAt time.Time
}

// NewTicket выпускает билет с 16-символьным кодом (base32, 80 бит энтропии).
func NewTicket(orderID, eventID uuid.UUID, seatID int64, now time.Time) Ticket {
	var b [10]byte
	_, _ = rand.Read(b[:])
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
	return Ticket{ID: uuid.New(), OrderID: orderID, EventID: eventID, SeatID: seatID, Code: code, IssuedAt: now}
}

// ToAPI — DTO.
func (t Ticket) ToAPI() api.Ticket {
	return api.Ticket{ID: t.ID, OrderID: t.OrderID, EventID: t.EventID, SeatID: t.SeatID, Code: t.Code, IssuedAt: t.IssuedAt}
}
