// Package app — сценарии использования каталога.
package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	bookingapi "github.com/nikolaysavelev/soldout/internal/booking/api"
	"github.com/nikolaysavelev/soldout/internal/catalog/api"
	"github.com/nikolaysavelev/soldout/internal/catalog/domain"
)

// Repository — хранилище каталога (реализация в adapters/pg).
type Repository interface {
	ListEvents(ctx context.Context) ([]domain.Event, error)
	GetEvent(ctx context.Context, id uuid.UUID) (domain.Event, error)
	GetSeat(ctx context.Context, id int64) (domain.Seat, error)
	SectorsByVenue(ctx context.Context, venueID uuid.UUID) ([]domain.Sector, error)
	SeatsByVenue(ctx context.Context, venueID uuid.UUID) ([]domain.Seat, error)
	VenueExists(ctx context.Context, id uuid.UUID) (bool, error)
	InsertEvent(ctx context.Context, e domain.Event) error
	UpdateSalesState(ctx context.Context, id uuid.UUID, state api.SalesState) error
}

// Service — реализация api.Service. Статусы мест (held/sold) принадлежат booking и читаются через его api.
type Service struct {
	repo   Repository
	states bookingapi.SeatStateReader
}

// New собирает сервис.
func New(repo Repository, states bookingapi.SeatStateReader) *Service {
	return &Service{repo: repo, states: states}
}

var _ api.Service = (*Service)(nil)

// ListEvents — все мероприятия.
func (s *Service) ListEvents(ctx context.Context) ([]api.Event, error) {
	events, err := s.repo.ListEvents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.Event, 0, len(events))
	for _, e := range events {
		out = append(out, e.ToAPI())
	}
	return out, nil
}

// GetEvent — мероприятие по id.
func (s *Service) GetEvent(ctx context.Context, id uuid.UUID) (api.Event, error) {
	e, err := s.repo.GetEvent(ctx, id)
	if err != nil {
		return api.Event{}, err
	}
	return e.ToAPI(), nil
}

// GetSeat — место по id.
func (s *Service) GetSeat(ctx context.Context, seatID int64) (api.Seat, error) {
	seat, err := s.repo.GetSeat(ctx, seatID)
	if err != nil {
		return api.Seat{}, err
	}
	return seat.ToAPI(), nil
}

// SeatMap — полная карта зала. На L1 собирается и сериализуется целиком на каждый запрос (ADR-001).
func (s *Service) SeatMap(ctx context.Context, eventID uuid.UUID) (api.SeatMap, error) {
	ev, err := s.repo.GetEvent(ctx, eventID)
	if err != nil {
		return api.SeatMap{}, err
	}
	sectors, err := s.repo.SectorsByVenue(ctx, ev.VenueID)
	if err != nil {
		return api.SeatMap{}, err
	}
	seats, err := s.repo.SeatsByVenue(ctx, ev.VenueID)
	if err != nil {
		return api.SeatMap{}, err
	}
	states, err := s.states.SeatStates(ctx, eventID)
	if err != nil {
		return api.SeatMap{}, fmt.Errorf("catalog: статусы мест: %w", err)
	}
	mapped := make(map[int64]api.SeatStatus, len(states))
	for id, st := range states {
		switch st {
		case bookingapi.SeatStateHeld:
			mapped[id] = api.SeatHeld
		case bookingapi.SeatStateSold:
			mapped[id] = api.SeatSold
		}
	}
	return domain.BuildSeatMap(eventID, sectors, seats, mapped), nil
}

// CreateEvent — админ: создать мероприятие на существующей площадке.
func (s *Service) CreateEvent(ctx context.Context, in api.CreateEventInput) (api.Event, error) {
	e, err := domain.NewEvent(in)
	if err != nil {
		return api.Event{}, err
	}
	ok, err := s.repo.VenueExists(ctx, e.VenueID)
	if err != nil {
		return api.Event{}, err
	}
	if !ok {
		return api.Event{}, api.ErrVenueNotFound
	}
	if err := s.repo.InsertEvent(ctx, e); err != nil {
		return api.Event{}, err
	}
	return e.ToAPI(), nil
}

// OpenSales — админ: открыть продажи.
func (s *Service) OpenSales(ctx context.Context, id uuid.UUID) (api.Event, error) {
	e, err := s.repo.GetEvent(ctx, id)
	if err != nil {
		return api.Event{}, err
	}
	if err := e.Open(); err != nil {
		return api.Event{}, err
	}
	if err := s.repo.UpdateSalesState(ctx, e.ID, e.SalesState); err != nil {
		return api.Event{}, err
	}
	return e.ToAPI(), nil
}
