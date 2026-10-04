// Package pg — таблица notifications.
package pg

import (
	"context"
	"fmt"

	"github.com/nikolaysavelev/soldout/internal/notification/api"
	"github.com/nikolaysavelev/soldout/internal/platform/db"
)

// Repo — реализация app.Repository.
type Repo struct{ db db.DBTX }

// New создаёт репозиторий.
func New(d db.DBTX) *Repo { return &Repo{db: d} }

// Insert — записать уведомление.
func (r *Repo) Insert(ctx context.Context, n api.Notification) error {
	_, err := r.db.Exec(ctx, `/* notification.insert */
		INSERT INTO notifications (id, user_id, kind, payload, status, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		n.ID, n.UserID, n.Kind, n.Payload, n.Status, n.CreatedAt)
	if err != nil {
		return fmt.Errorf("notification/pg: insert: %w", err)
	}
	return nil
}
