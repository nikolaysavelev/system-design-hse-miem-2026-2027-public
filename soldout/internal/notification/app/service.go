// Package app — доставка уведомлений: запись в таблицу + отправка через эмулятор (лог).
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/notification/api"
	ticketingapi "github.com/nikolaysavelev/soldout/internal/ticketing/api"
)

// Sender — канал доставки (эмулятор e-mail/push; на L1 — лог).
type Sender interface {
	Send(ctx context.Context, n api.Notification) error
}

// Repository — таблица notifications.
type Repository interface {
	Insert(ctx context.Context, n api.Notification) error
}

// Service — реализация ticketingapi.Notifier.
type Service struct {
	sender Sender
	repo   Repository
	now    func() time.Time
}

// New собирает сервис.
func New(sender Sender, repo Repository) *Service {
	return &Service{sender: sender, repo: repo, now: time.Now}
}

var _ ticketingapi.Notifier = (*Service)(nil)

// Send — отправить и записать результат.
func (s *Service) Send(ctx context.Context, in ticketingapi.Notification) error {
	n := api.Notification{ID: uuid.New(), UserID: in.UserID, Kind: in.Kind, Payload: in.Payload, CreatedAt: s.now(), Status: "sent"}
	if err := s.sender.Send(ctx, n); err != nil {
		n.Status = "failed"
	}
	return s.repo.Insert(ctx, n)
}
