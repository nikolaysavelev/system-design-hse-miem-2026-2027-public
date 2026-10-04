// Package app — идемпотентное списание через эмулятор PSP.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	bookingapi "github.com/nikolaysavelev/soldout/internal/booking/api"
	"github.com/nikolaysavelev/soldout/internal/payment/api"
	"github.com/nikolaysavelev/soldout/internal/payment/domain"
)

// PSP — платёжный провайдер (эмулятор в adapters/psp).
type PSP interface {
	Charge(ctx context.Context, orderID uuid.UUID, amountMinor int64) (domain.PSPResult, error)
}

// Repository — таблица payments.
type Repository interface {
	Insert(ctx context.Context, p domain.Payment) error
	GetByKey(ctx context.Context, key string) (domain.Payment, error)
	GetByOrder(ctx context.Context, orderID uuid.UUID) (domain.Payment, error)
}

// Service — реализация bookingapi.PaymentGateway и api.Service.
type Service struct {
	psp  PSP
	repo Repository
	now  func() time.Time
}

// New собирает сервис.
func New(psp PSP, repo Repository) *Service { return &Service{psp: psp, repo: repo, now: time.Now} }

var (
	_ bookingapi.PaymentGateway = (*Service)(nil)
	_ api.Service               = (*Service)(nil)
)

// Charge — списание. Повтор с тем же IdempotencyKey возвращает сохранённый результат без обращения к PSP.
func (s *Service) Charge(ctx context.Context, req bookingapi.ChargeRequest) (bookingapi.ChargeResult, error) {
	if p, err := s.repo.GetByKey(ctx, req.IdempotencyKey); err == nil {
		return toResult(p), nil
	} else if !errors.Is(err, api.ErrNotFound) {
		return bookingapi.ChargeResult{}, err
	}
	res, err := s.psp.Charge(ctx, req.OrderID, req.AmountMinor)
	if err != nil {
		return bookingapi.ChargeResult{}, err
	}
	p := domain.NewPayment(req.OrderID, req.IdempotencyKey, res, s.now())
	if err := s.repo.Insert(ctx, p); err != nil {
		if errors.Is(err, ErrDuplicateKey) { // гонка повторов: берём сохранённый
			if p2, err2 := s.repo.GetByKey(ctx, req.IdempotencyKey); err2 == nil {
				return toResult(p2), nil
			}
		}
		return bookingapi.ChargeResult{}, err
	}
	return toResult(p), nil
}

// ErrDuplicateKey — платёж с таким ключом уже сохранён.
var ErrDuplicateKey = errors.New("payment: дубликат idempotency_key")

// GetByOrder — платёж по заказу.
func (s *Service) GetByOrder(ctx context.Context, orderID uuid.UUID) (api.Payment, error) {
	p, err := s.repo.GetByOrder(ctx, orderID)
	if err != nil {
		return api.Payment{}, err
	}
	return p.ToAPI(), nil
}

func toResult(p domain.Payment) bookingapi.ChargeResult {
	return bookingapi.ChargeResult{Succeeded: p.Status == api.Succeeded, PSPRef: p.PSPRef, Reason: p.Reason}
}
