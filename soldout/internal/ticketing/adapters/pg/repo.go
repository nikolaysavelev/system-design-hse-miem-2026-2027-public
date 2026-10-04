// Package pg — таблица tickets.
package pg

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/platform/db"
	"github.com/nikolaysavelev/soldout/internal/ticketing/domain"
)

// Repo — реализация app.Repository.
type Repo struct{ db db.DBTX }

// New создаёт репозиторий.
func New(d db.DBTX) *Repo { return &Repo{db: d} }

// InsertIfAbsent — ON CONFLICT (order_id, seat_id) DO NOTHING.
func (r *Repo) InsertIfAbsent(ctx context.Context, t domain.Ticket) (bool, error) {
	tag, err := r.db.Exec(ctx, `/* ticketing.insert */
		INSERT INTO tickets (id, order_id, seat_id, code, issued_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (order_id, seat_id) DO NOTHING`, t.ID, t.OrderID, t.SeatID, t.Code, t.IssuedAt)
	if err != nil {
		return false, fmt.Errorf("ticketing/pg: insert: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ListByOrders — билеты по списку заказов.
func (r *Repo) ListByOrders(ctx context.Context, orderIDs []uuid.UUID) ([]domain.Ticket, error) {
	rows, err := r.db.Query(ctx, `/* ticketing.list_by_orders */
		SELECT id, order_id, seat_id, code, issued_at FROM tickets WHERE order_id = ANY($1) ORDER BY issued_at, seat_id`, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("ticketing/pg: list: %w", err)
	}
	defer rows.Close()
	var out []domain.Ticket
	for rows.Next() {
		var t domain.Ticket
		if err := rows.Scan(&t.ID, &t.OrderID, &t.SeatID, &t.Code, &t.IssuedAt); err != nil {
			return nil, fmt.Errorf("ticketing/pg: scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
