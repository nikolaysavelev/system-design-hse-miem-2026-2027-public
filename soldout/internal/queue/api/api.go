// Package api — публичный контракт модуля queue (waiting room).
// На занятии 1 очередь упрощена: Join всегда выдаёт токен допуска (позиция 0); настоящая очередь
// на Valkey sorted set и admission rate появляются на занятии 2.
package api

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Admission — допуск к бронированию. При QUEUE_ENABLED=true Join возвращает Position > 0 и пустой Token:
// токен появится в Status после допуска admitter'ом.
type Admission struct {
	Token     string    `json:"token"`
	EventID   uuid.UUID `json:"event_id"`
	UserID    uuid.UUID `json:"user_id"`
	Position  int       `json:"position"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Status — состояние пользователя в очереди.
type Status struct {
	EventID  uuid.UUID `json:"event_id"`
	UserID   uuid.UUID `json:"user_id"`
	Admitted bool      `json:"admitted"`
	Position int64     `json:"position"`        // 0 — допущен или не в очереди
	Token    string    `json:"token,omitempty"` // при Admitted
}

// Stats — размеры очереди мероприятия.
type Stats struct {
	EventID  uuid.UUID `json:"event_id"`
	Queue    int64     `json:"queue"`
	Inflight int64     `json:"inflight"`
	Enabled  bool      `json:"queue_enabled"`
	Rate     float64   `json:"admission_rate"`
}

// Ошибки модуля.
var (
	ErrAdmissionInvalid = errors.New("queue: токен допуска отсутствует, недействителен или истёк")
)

// Service — waiting room.
type Service interface {
	// Join ставит пользователя в очередь и (на L1 — сразу) выдаёт токен допуска.
	Join(ctx context.Context, eventID, userID uuid.UUID) (Admission, error)
	// Validate проверяет, что токен выдан этому пользователю на это мероприятие и не истёк.
	Validate(ctx context.Context, token string, eventID, userID uuid.UUID) error
	// Status — позиция в очереди или выданный допуск (QUEUE_ENABLED=true).
	Status(ctx context.Context, eventID, userID uuid.UUID) (Status, error)
	// Stats — размеры очереди.
	Stats(ctx context.Context, eventID uuid.UUID) (Stats, error)
}
