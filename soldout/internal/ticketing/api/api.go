// Package api — публичный контракт модуля ticketing. Выпуск инициирует booking через порт
// booking/api.TicketIssuer; уведомления уходят через порт Notifier (реализует notification/app).
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

// Notification — сообщение пользователю.
type Notification struct {
	UserID  uuid.UUID
	Kind    string
	Payload map[string]any
}

// Notifier — порт доставки уведомлений.
type Notifier interface {
	Send(ctx context.Context, n Notification) error
}

// Service — чтение билетов по заказам (заказы пользователя отдаёт booking.api).
type Service interface {
	ListByOrders(ctx context.Context, orderIDs []uuid.UUID) ([]Ticket, error)
}
