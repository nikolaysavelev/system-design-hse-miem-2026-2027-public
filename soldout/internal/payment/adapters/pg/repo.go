// Package pg — таблица payments.
package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nikolaysavelev/soldout/internal/payment/api"
	"github.com/nikolaysavelev/soldout/internal/payment/app"
	"github.com/nikolaysavelev/soldout/internal/payment/domain"
	"github.com/nikolaysavelev/soldout/internal/platform/db"
)

// Repo — реализация app.Repository.
type Repo struct{ db db.DBTX }

// New создаёт репозиторий.
func New(d db.DBTX) *Repo { return &Repo{db: d} }

// Insert — сохранить платёж.
func (r *Repo) Insert(ctx context.Context, p domain.Payment) error {
	_, err := r.db.Exec(ctx, `/* payment.insert */
		INSERT INTO payments (id, order_id, status, psp_ref, idempotency_key, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		p.ID, p.OrderID, p.Status, nullable(p.PSPRef), p.IdempotencyKey, p.CreatedAt)
	if db.IsUniqueViolation(err, "payments_idempotency_key_key") {
		return app.ErrDuplicateKey
	}
	if err != nil {
		return fmt.Errorf("payment/pg: insert: %w", err)
	}
	return nil
}

const cols = "id, order_id, status, coalesce(psp_ref, ''), idempotency_key, created_at"

func (r *Repo) get(ctx context.Context, name, where string, arg any) (domain.Payment, error) {
	var p domain.Payment
	err := r.db.QueryRow(ctx, fmt.Sprintf("/* %s */ SELECT %s FROM payments WHERE %s ORDER BY created_at DESC LIMIT 1", name, cols, where), arg).
		Scan(&p.ID, &p.OrderID, &p.Status, &p.PSPRef, &p.IdempotencyKey, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, api.ErrNotFound
	}
	if err != nil {
		return p, fmt.Errorf("payment/pg: get: %w", err)
	}
	return p, nil
}

// GetByKey — платёж по ключу идемпотентности.
func (r *Repo) GetByKey(ctx context.Context, key string) (domain.Payment, error) {
	return r.get(ctx, "payment.get_by_key", "idempotency_key = $1", key)
}

// GetByOrder — последний платёж заказа.
func (r *Repo) GetByOrder(ctx context.Context, orderID uuid.UUID) (domain.Payment, error) {
	return r.get(ctx, "payment.get_by_order", "order_id = $1", orderID)
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
