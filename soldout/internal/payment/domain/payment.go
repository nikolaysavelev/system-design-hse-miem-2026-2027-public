// Package domain — платёж как результат обращения к PSP.
package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/payment/api"
)

// Payment — платёж.
type Payment struct {
	ID             uuid.UUID
	OrderID        uuid.UUID
	Status         api.Status
	PSPRef         string
	Reason         string
	IdempotencyKey string
	CreatedAt      time.Time
}

// PSPResult — ответ платёжного провайдера.
type PSPResult struct {
	Succeeded bool
	Ref       string
	Reason    string
}

// NewPayment фиксирует результат PSP.
func NewPayment(orderID uuid.UUID, key string, res PSPResult, now time.Time) Payment {
	p := Payment{ID: uuid.New(), OrderID: orderID, IdempotencyKey: key, CreatedAt: now, PSPRef: res.Ref, Reason: res.Reason, Status: api.Failed}
	if res.Succeeded {
		p.Status = api.Succeeded
	}
	return p
}

// ToAPI — DTO.
func (p Payment) ToAPI() api.Payment {
	return api.Payment{ID: p.ID, OrderID: p.OrderID, Status: p.Status, PSPRef: p.PSPRef, IdempotencyKey: p.IdempotencyKey, CreatedAt: p.CreatedAt}
}
