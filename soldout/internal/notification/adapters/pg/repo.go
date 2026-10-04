// Package pg — журнал уведомлений (notifications) и обработанные события (notifier.processed_events).
package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nikolaysavelev/soldout/internal/notification/api"
	"github.com/nikolaysavelev/soldout/internal/notification/app"
	"github.com/nikolaysavelev/soldout/internal/platform/db"
)

// Repo — реализация app.Store.
type Repo struct{ pool *db.Pool }

// New создаёт репозиторий.
func New(pool *db.Pool) *Repo { return &Repo{pool: pool} }

var _ app.Store = (*Repo)(nil)

// Accept — в одной транзакции: событие отмечено обработанным, уведомление pending создано (или уже было).
// Возвращает уведомление по (order_id, kind): при повторной доставке — существующее с его статусом.
func (r *Repo) Accept(ctx context.Context, consumer string, n api.Notification) (api.Notification, error) {
	var out api.Notification
	err := db.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		var fresh bool
		err := tx.QueryRow(ctx, `/* notifier.processed_insert */
			INSERT INTO notifier.processed_events (event_id, consumer) VALUES ($1, $2)
			ON CONFLICT DO NOTHING RETURNING true`, n.EventID, consumer).Scan(&fresh)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("processed_events: %w", err)
		}
		if fresh {
			if _, err := tx.Exec(ctx, `/* notifier.notification_insert */
				INSERT INTO notifications (id, order_id, event_id, user_id, kind, payload, status, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
				ON CONFLICT (order_id, kind) DO NOTHING`,
				n.ID, n.OrderID, n.EventID, n.UserID, n.Kind, n.Payload, n.Status, n.CreatedAt); err != nil {
				return fmt.Errorf("notifications insert: %w", err)
			}
		}
		return tx.QueryRow(ctx, `/* notifier.notification_get */
			SELECT id, order_id, coalesce(event_id, '00000000-0000-0000-0000-000000000000'::uuid), user_id, kind, payload, status, created_at
			FROM notifications WHERE order_id = $1 AND kind = $2`, n.OrderID, n.Kind).
			Scan(&out.ID, &out.OrderID, &out.EventID, &out.UserID, &out.Kind, &out.Payload, &out.Status, &out.CreatedAt)
	})
	if err != nil {
		return api.Notification{}, fmt.Errorf("notification/pg: accept: %w", err)
	}
	return out, nil
}

// SetStatus — результат попытки отправки.
func (r *Repo) SetStatus(ctx context.Context, id uuid.UUID, status string, attempts int) error {
	_, err := r.pool.Exec(ctx, `/* notifier.notification_status */
		UPDATE notifications SET status = $2, attempts = greatest(attempts, $3), updated_at = now() WHERE id = $1`,
		id, status, attempts)
	if err != nil {
		return fmt.Errorf("notification/pg: status: %w", err)
	}
	return nil
}
