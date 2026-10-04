// Package domain — сущности и правила каталога: мероприятия, площадки, секторы, места.
package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/catalog/api"
)

// Event — мероприятие.
type Event struct {
	ID          uuid.UUID
	Name        string
	VenueID     uuid.UUID
	StartsAt    time.Time
	SalesOpenAt time.Time
	SalesState  api.SalesState
}

// NewEvent валидирует и создаёт мероприятие в состоянии scheduled.
func NewEvent(in api.CreateEventInput) (Event, error) {
	name := strings.TrimSpace(in.Name)
	switch {
	case name == "":
		return Event{}, fmt.Errorf("%w: пустое название", api.ErrValidation)
	case in.VenueID == uuid.Nil:
		return Event{}, fmt.Errorf("%w: не указана площадка", api.ErrValidation)
	case in.StartsAt.IsZero() || in.SalesOpenAt.IsZero():
		return Event{}, fmt.Errorf("%w: не указаны даты", api.ErrValidation)
	case in.SalesOpenAt.After(in.StartsAt):
		return Event{}, fmt.Errorf("%w: продажи не могут открываться после начала", api.ErrValidation)
	}
	return Event{
		ID: uuid.New(), Name: name, VenueID: in.VenueID,
		StartsAt: in.StartsAt, SalesOpenAt: in.SalesOpenAt, SalesState: api.SalesScheduled,
	}, nil
}

// Open переводит продажи в состояние open. Разрешено только из scheduled; open → open идемпотентно.
func (e *Event) Open() error {
	switch e.SalesState {
	case api.SalesScheduled, api.SalesOpen:
		e.SalesState = api.SalesOpen
		return nil
	default:
		return fmt.Errorf("%w: из %s в open", api.ErrInvalidTransition, e.SalesState)
	}
}

// Close закрывает продажи; из closed — идемпотентно.
func (e *Event) Close() error {
	e.SalesState = api.SalesClosed
	return nil
}

// IsOpen — можно ли бронировать.
func (e Event) IsOpen() bool { return e.SalesState == api.SalesOpen }

// ToAPI — DTO.
func (e Event) ToAPI() api.Event {
	return api.Event{ID: e.ID, Name: e.Name, VenueID: e.VenueID, StartsAt: e.StartsAt, SalesOpenAt: e.SalesOpenAt, SalesState: e.SalesState}
}
