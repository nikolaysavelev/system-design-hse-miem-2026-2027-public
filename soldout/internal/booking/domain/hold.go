// Package domain — правила бронирования: hold с истечением, лимит билетов, заказ.
package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/booking/api"
)

// Hold — удержание места.
type Hold struct {
	ID        uuid.UUID
	EventID   uuid.UUID
	SeatID    int64
	UserID    uuid.UUID
	Status    api.HoldStatus
	ExpiresAt time.Time
	CreatedAt time.Time
	// HW1, V2: цена, зафиксированная в момент hold, и порядковый номер hold в секторе. nil — у сектора нет ценовой политики.
	PriceMinor *int64
	Ordinal    *int
}

// NewHold создаёт активный hold на ttl.
func NewHold(eventID uuid.UUID, seatID int64, userID uuid.UUID, now time.Time, ttl time.Duration) Hold {
	return Hold{
		ID: uuid.New(), EventID: eventID, SeatID: seatID, UserID: userID,
		Status: api.HoldActive, ExpiresAt: now.Add(ttl), CreatedAt: now,
	}
}

// SetPrice фиксирует порядковый номер в секторе и цену по политике.
func (h *Hold) SetPrice(p Pricing, ordinal int) {
	price := p.PriceFor(ordinal)
	h.PriceMinor, h.Ordinal = &price, &ordinal
}

// IsExpired — срок удержания вышел (для active).
func (h Hold) IsExpired(now time.Time) bool { return !now.Before(h.ExpiresAt) }

// IsUsable — активен и не истёк: можно включить в заказ / подтвердить.
func (h Hold) IsUsable(now time.Time) bool { return h.Status == api.HoldActive && !h.IsExpired(now) }

// Release снимает удержание. Разрешено только из active.
func (h *Hold) Release() error {
	if h.Status != api.HoldActive {
		return fmt.Errorf("%w: статус %s", api.ErrHoldNotActive, h.Status)
	}
	h.Status = api.HoldReleased
	return nil
}

// Confirm подтверждает удержание после оплаты. Разрешено только из active; истёкший active подтвердить нельзя.
func (h *Hold) Confirm(now time.Time) error {
	if h.Status != api.HoldActive {
		return fmt.Errorf("%w: статус %s", api.ErrHoldNotActive, h.Status)
	}
	if h.IsExpired(now) {
		return api.ErrHoldExpired
	}
	h.Status = api.HoldConfirmed
	return nil
}

// CanHoldMore — FR-7: пользователь с current активными/подтверждёнными hold может взять ещё одно.
func CanHoldMore(current int) bool { return current < api.MaxTicketsPerUser }

// ToAPI — DTO.
func (h Hold) ToAPI() api.Hold {
	return api.Hold{ID: h.ID, EventID: h.EventID, SeatID: h.SeatID, UserID: h.UserID, Status: h.Status, ExpiresAt: h.ExpiresAt, CreatedAt: h.CreatedAt,
		PriceMinor: h.PriceMinor, Ordinal: h.Ordinal}
}
