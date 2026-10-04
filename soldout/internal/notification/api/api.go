// Package api — публичный контракт модуля notification. С занятия 4 модуль работает отдельным сервисом
// cmd/notifier: принимает событие booking/api.OrderPaidEvent (Kafka или polling outbox) и отправляет письмо.
package api

import (
	"time"

	"github.com/google/uuid"
)

// Kind — вид уведомления об оплаченном заказе (инвариант I5: ровно одно на paid заказ).
const KindTicketsIssued = "tickets_issued"

// Статусы уведомления.
const (
	StatusPending = "pending" // принято, письмо ещё не ушло
	StatusSent    = "sent"
	StatusFailed  = "failed" // шлюз отказал; повтор — следующей доставкой того же события
)

// Notification — запись журнала уведомлений.
type Notification struct {
	ID        uuid.UUID      `json:"id"`
	OrderID   uuid.UUID      `json:"order_id"`
	EventID   uuid.UUID      `json:"event_id"` // id события outbox
	UserID    uuid.UUID      `json:"user_id"`
	Kind      string         `json:"kind"`
	Payload   map[string]any `json:"payload"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
}
