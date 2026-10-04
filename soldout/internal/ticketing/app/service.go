// Package app — выпуск билетов по оплаченному заказу и их чтение.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	bookingapi "github.com/nikolaysavelev/soldout/internal/booking/api"
	"github.com/nikolaysavelev/soldout/internal/ticketing/api"
	"github.com/nikolaysavelev/soldout/internal/ticketing/domain"
)

// Repository — таблица tickets.
type Repository interface {
	// InsertIfAbsent вставляет билет, если на (order_id, seat_id) его ещё нет; возвращает true при вставке.
	InsertIfAbsent(ctx context.Context, t domain.Ticket) (bool, error)
	ListByOrders(ctx context.Context, orderIDs []uuid.UUID) ([]domain.Ticket, error)
}

// Service — реализация bookingapi.TicketIssuer и api.Service.
type Service struct {
	repo     Repository
	notifier api.Notifier
	logger   *slog.Logger
	now      func() time.Time
}

// New собирает сервис.
func New(repo Repository, notifier api.Notifier, logger *slog.Logger) *Service {
	return &Service{repo: repo, notifier: notifier, logger: logger, now: time.Now}
}

var (
	_ bookingapi.TicketIssuer = (*Service)(nil)
	_ api.Service             = (*Service)(nil)
)

// Issue — FR-5: билет на каждое место заказа; идемпотентен по (order_id, seat_id). Уведомление —
// только если выпущен хотя бы один новый билет.
func (s *Service) Issue(ctx context.Context, req bookingapi.IssueRequest) error {
	if len(req.SeatIDs) == 0 {
		return fmt.Errorf("ticketing: заказ %s без мест", req.OrderID)
	}
	var issued []string
	for _, seatID := range req.SeatIDs {
		t := domain.NewTicket(req.OrderID, req.EventID, seatID, s.now())
		inserted, err := s.repo.InsertIfAbsent(ctx, t)
		if err != nil {
			return err
		}
		if inserted {
			issued = append(issued, t.Code)
		}
	}
	if len(issued) == 0 {
		return nil
	}
	err := s.notifier.Send(ctx, api.Notification{
		UserID: req.UserID, Kind: "tickets_issued",
		Payload: map[string]any{"order_id": req.OrderID.String(), "event_id": req.EventID.String(), "codes": issued},
	})
	if err != nil {
		// билеты уже выпущены; потеря уведомления — известная проблема L1, решается outbox'ом на занятии 3
		s.logger.ErrorContext(ctx, "ticketing: уведомление не отправлено", "order_id", req.OrderID, "err", err)
	}
	return nil
}

// ListByOrders — билеты по списку заказов.
func (s *Service) ListByOrders(ctx context.Context, orderIDs []uuid.UUID) ([]api.Ticket, error) {
	if len(orderIDs) == 0 {
		return []api.Ticket{}, nil
	}
	tickets, err := s.repo.ListByOrders(ctx, orderIDs)
	if err != nil {
		return nil, err
	}
	out := make([]api.Ticket, 0, len(tickets))
	for _, t := range tickets {
		out = append(out, t.ToAPI())
	}
	return out, nil
}
