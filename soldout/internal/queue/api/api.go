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

// Admission — допуск к бронированию.
type Admission struct {
	Token     string    `json:"token"`
	EventID   uuid.UUID `json:"event_id"`
	UserID    uuid.UUID `json:"user_id"`
	Position  int       `json:"position"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
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
}
