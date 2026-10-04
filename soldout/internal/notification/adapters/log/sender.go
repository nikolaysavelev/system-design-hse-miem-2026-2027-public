// Package log — эмулятор доставки: уведомление пишется в структурный лог.
package log

import (
	"context"
	"log/slog"

	"github.com/nikolaysavelev/soldout/internal/notification/api"
)

// Sender — реализация app.Sender.
type Sender struct{ logger *slog.Logger }

// New создаёт отправитель.
func New(logger *slog.Logger) *Sender { return &Sender{logger: logger} }

// Send — «доставка».
func (s *Sender) Send(ctx context.Context, n api.Notification) error {
	s.logger.InfoContext(ctx, "notification", "id", n.ID, "user_id", n.UserID, "kind", n.Kind, "payload", n.Payload)
	return nil
}
