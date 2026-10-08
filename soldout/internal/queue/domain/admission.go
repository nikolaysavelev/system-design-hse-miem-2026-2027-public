// Package domain — правила допуска waiting room.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/queue/api"
)

// Admission — допуск.
type Admission struct {
	Token     string
	EventID   uuid.UUID
	UserID    uuid.UUID
	Position  int
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// NewAdmission выдаёт новый допуск со случайным токеном на ttl.
func NewAdmission(eventID, userID uuid.UUID, now time.Time, ttl time.Duration) Admission {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return Admission{
		Token: hex.EncodeToString(b[:]), EventID: eventID, UserID: userID,
		Position: 0, IssuedAt: now, ExpiresAt: now.Add(ttl),
	}
}

// ValidFor — токен принадлежит паре (event, user) и ещё действует.
func (a Admission) ValidFor(eventID, userID uuid.UUID, now time.Time) bool {
	return a.EventID == eventID && a.UserID == userID && now.Before(a.ExpiresAt)
}

// ToAPI — DTO.
func (a Admission) ToAPI() api.Admission {
	return api.Admission{Token: a.Token, EventID: a.EventID, UserID: a.UserID, Position: a.Position, IssuedAt: a.IssuedAt, ExpiresAt: a.ExpiresAt}
}
