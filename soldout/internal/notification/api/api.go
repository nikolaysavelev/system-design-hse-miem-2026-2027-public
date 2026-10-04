// Package api — публичный контракт модуля notification. Отправку инициирует ticketing через порт
// ticketing/api.Notifier, который реализует notification/app.
package api

import (
	"time"

	"github.com/google/uuid"
)

// Notification — запись журнала уведомлений.
type Notification struct {
	ID        uuid.UUID      `json:"id"`
	UserID    uuid.UUID      `json:"user_id"`
	Kind      string         `json:"kind"`
	Payload   map[string]any `json:"payload"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
}
