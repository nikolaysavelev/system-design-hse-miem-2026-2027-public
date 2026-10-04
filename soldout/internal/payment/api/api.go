// Package api — публичный контракт модуля payment. Списание инициирует booking через порт
// booking/api.PaymentGateway, который реализует payment/app; здесь — DTO для чтения.
package api

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status — исход платежа.
type Status string

const (
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
)

// Payment — платёж.
type Payment struct {
	ID             uuid.UUID `json:"id"`
	OrderID        uuid.UUID `json:"order_id"`
	Status         Status    `json:"status"`
	PSPRef         string    `json:"psp_ref,omitempty"`
	IdempotencyKey string    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
}

// ErrNotFound — платёж не найден.
var ErrNotFound = errors.New("payment: платёж не найден")

// Service — чтение платежей.
type Service interface {
	GetByOrder(ctx context.Context, orderID uuid.UUID) (Payment, error)
}
