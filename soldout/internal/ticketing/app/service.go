// Package app — выпуск билетов по оплаченному заказу и их чтение.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	bookingapi "github.com/nikolaysavelev/soldout/internal/booking/api"
	"github.com/nikolaysavelev/soldout/internal/platform/otel"
	"github.com/nikolaysavelev/soldout/internal/ticketing/api"
	"github.com/nikolaysavelev/soldout/internal/ticketing/domain"
)

// Repository — таблица tickets.
type Repository interface {
	// InsertIfAbsent вставляет билет, если на (order_id, seat_id) его ещё нет; возвращает true при вставке.
	InsertIfAbsent(ctx context.Context, t domain.Ticket) (bool, error)
	ListByOrders(ctx context.Context, orderIDs []uuid.UUID) ([]domain.Ticket, error)
}

// Service — реализация bookingapi.TicketIssuer и api.Service. С занятия 4 ticketing не знает о notification:
// письмо уходит из события OrderPaid (outbox → Kafka → notifier).
type Service struct {
	repo   Repository
	logger *slog.Logger
	now    func() time.Time
}

// New собирает сервис.
func New(repo Repository, logger *slog.Logger) *Service {
	return &Service{repo: repo, logger: logger, now: time.Now}
}

var (
	_ bookingapi.TicketIssuer = (*Service)(nil)
	_ api.Service             = (*Service)(nil)
)

// Issue — FR-5: билет на каждое место заказа; идемпотентен по (order_id, seat_id).
func (s *Service) Issue(ctx context.Context, req bookingapi.IssueRequest) (err error) {
	ctx, span := otel.Start(ctx, "ticketing.Issue", otel.String("order.id", req.OrderID.String()), otel.Int("tickets.seats", len(req.SeatIDs)))
	defer otel.End(span, &err)
	if len(req.SeatIDs) == 0 {
		return fmt.Errorf("ticketing: заказ %s без мест", req.OrderID)
	}
	for _, seatID := range req.SeatIDs {
		t := domain.NewTicket(req.OrderID, req.EventID, seatID, s.now())
		if _, err := s.repo.InsertIfAbsent(ctx, t); err != nil {
			return err
		}
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
