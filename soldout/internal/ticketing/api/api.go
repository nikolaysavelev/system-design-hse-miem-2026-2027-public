// Package api — публичный контракт модуля ticketing. Выпуск инициирует booking через порт
// booking/api.TicketIssuer. С занятия 4 уведомления не часть ticketing: их шлёт notifier по событию OrderPaid.
package api

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Ticket — билет.
type Ticket struct {
	ID       uuid.UUID `json:"id"`
	OrderID  uuid.UUID `json:"order_id"`
	EventID  uuid.UUID `json:"event_id"`
	SeatID   int64     `json:"seat_id"`
	Code     string    `json:"code"`
	IssuedAt time.Time `json:"issued_at"`
}

// Service — чтение билетов по заказам (заказы пользователя отдаёт booking.api).
type Service interface {
	ListByOrders(ctx context.Context, orderIDs []uuid.UUID) ([]Ticket, error)
}
