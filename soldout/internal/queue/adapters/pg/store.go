// Package pg — таблица admissions.
package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nikolaysavelev/soldout/internal/platform/db"
	"github.com/nikolaysavelev/soldout/internal/queue/app"
	"github.com/nikolaysavelev/soldout/internal/queue/domain"
)

// Store — реализация app.Store.
type Store struct{ db db.DBTX }

// New создаёт хранилище.
func New(d db.DBTX) *Store { return &Store{db: d} }

// Insert — новый допуск.
func (s *Store) Insert(ctx context.Context, a domain.Admission) error {
	_, err := s.db.Exec(ctx, `/* queue.insert_admission */
		INSERT INTO admissions (token, event_id, user_id, issued_at, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		a.Token, a.EventID, a.UserID, a.IssuedAt, a.ExpiresAt)
	if err != nil {
		return fmt.Errorf("queue/pg: insert: %w", err)
	}
	return nil
}

// Get — допуск по токену.
func (s *Store) Get(ctx context.Context, token string) (domain.Admission, error) {
	var a domain.Admission
	err := s.db.QueryRow(ctx, `/* queue.get_admission */
		SELECT token, event_id, user_id, issued_at, expires_at FROM admissions WHERE token = $1`, token).
		Scan(&a.Token, &a.EventID, &a.UserID, &a.IssuedAt, &a.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, app.ErrNotFound
	}
	if err != nil {
		return a, fmt.Errorf("queue/pg: get: %w", err)
	}
	return a, nil
}
