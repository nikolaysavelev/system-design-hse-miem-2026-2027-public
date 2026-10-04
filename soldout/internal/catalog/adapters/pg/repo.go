// Package pg — PostgreSQL-хранилище каталога. Только таблицы venues, sectors, seats, events.
package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nikolaysavelev/soldout/internal/catalog/api"
	"github.com/nikolaysavelev/soldout/internal/catalog/domain"
	"github.com/nikolaysavelev/soldout/internal/platform/db"
)

// Repo — реализация app.Repository.
type Repo struct{ db db.DBTX }

// New создаёт репозиторий поверх пула.
func New(d db.DBTX) *Repo { return &Repo{db: d} }

const eventCols = "id, name, venue_id, starts_at, sales_open_at, sales_state"

func scanEvent(row pgx.Row) (domain.Event, error) {
	var e domain.Event
	err := row.Scan(&e.ID, &e.Name, &e.VenueID, &e.StartsAt, &e.SalesOpenAt, &e.SalesState)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, api.ErrEventNotFound
	}
	if err != nil {
		return e, fmt.Errorf("catalog/pg: scan event: %w", err)
	}
	return e, nil
}

// ListEvents — все мероприятия по дате начала.
func (r *Repo) ListEvents(ctx context.Context) ([]domain.Event, error) {
	rows, err := r.db.Query(ctx, "/* catalog.list_events */ SELECT "+eventCols+" FROM events ORDER BY starts_at")
	if err != nil {
		return nil, fmt.Errorf("catalog/pg: list events: %w", err)
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetEvent — мероприятие по id.
func (r *Repo) GetEvent(ctx context.Context, id uuid.UUID) (domain.Event, error) {
	return scanEvent(r.db.QueryRow(ctx, "/* catalog.get_event */ SELECT "+eventCols+" FROM events WHERE id = $1", id))
}

// GetSeat — место с площадкой (через сектор).
func (r *Repo) GetSeat(ctx context.Context, id int64) (domain.Seat, error) {
	var s domain.Seat
	err := r.db.QueryRow(ctx, `/* catalog.get_seat */
		SELECT s.id, s.sector_id, sec.venue_id, s.row_no, s.seat_no
		FROM seats s JOIN sectors sec ON sec.id = s.sector_id
		WHERE s.id = $1`, id).Scan(&s.ID, &s.SectorID, &s.VenueID, &s.RowNo, &s.SeatNo)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, api.ErrSeatNotFound
	}
	if err != nil {
		return s, fmt.Errorf("catalog/pg: get seat: %w", err)
	}
	return s, nil
}

// SectorsByVenue — секторы площадки.
func (r *Repo) SectorsByVenue(ctx context.Context, venueID uuid.UUID) ([]domain.Sector, error) {
	rows, err := r.db.Query(ctx, `/* catalog.sectors_by_venue */
		SELECT id, venue_id, name, kind, capacity FROM sectors WHERE venue_id = $1 ORDER BY name`, venueID)
	if err != nil {
		return nil, fmt.Errorf("catalog/pg: sectors: %w", err)
	}
	defer rows.Close()
	var out []domain.Sector
	for rows.Next() {
		var s domain.Sector
		if err := rows.Scan(&s.ID, &s.VenueID, &s.Name, &s.Kind, &s.Capacity); err != nil {
			return nil, fmt.Errorf("catalog/pg: scan sector: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SeatsByVenue — все места площадки (40 000 строк на каждый запрос карты зала — намеренно, ADR-001).
func (r *Repo) SeatsByVenue(ctx context.Context, venueID uuid.UUID) ([]domain.Seat, error) {
	rows, err := r.db.Query(ctx, `/* catalog.seats_by_venue */
		SELECT s.id, s.sector_id, sec.venue_id, s.row_no, s.seat_no
		FROM seats s JOIN sectors sec ON sec.id = s.sector_id
		WHERE sec.venue_id = $1
		ORDER BY s.id`, venueID)
	if err != nil {
		return nil, fmt.Errorf("catalog/pg: seats: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Seat, 0, 40000)
	for rows.Next() {
		var s domain.Seat
		if err := rows.Scan(&s.ID, &s.SectorID, &s.VenueID, &s.RowNo, &s.SeatNo); err != nil {
			return nil, fmt.Errorf("catalog/pg: scan seat: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// VenueExists — есть ли площадка.
func (r *Repo) VenueExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var ok bool
	if err := r.db.QueryRow(ctx, "/* catalog.venue_exists */ SELECT EXISTS (SELECT 1 FROM venues WHERE id = $1)", id).Scan(&ok); err != nil {
		return false, fmt.Errorf("catalog/pg: venue exists: %w", err)
	}
	return ok, nil
}

// InsertEvent — новое мероприятие.
func (r *Repo) InsertEvent(ctx context.Context, e domain.Event) error {
	_, err := r.db.Exec(ctx, `/* catalog.insert_event */
		INSERT INTO events (id, name, venue_id, starts_at, sales_open_at, sales_state)
		VALUES ($1, $2, $3, $4, $5, $6)`, e.ID, e.Name, e.VenueID, e.StartsAt, e.SalesOpenAt, e.SalesState)
	if err != nil {
		return fmt.Errorf("catalog/pg: insert event: %w", err)
	}
	return nil
}

// UpdateSalesState — смена состояния продаж.
func (r *Repo) UpdateSalesState(ctx context.Context, id uuid.UUID, state api.SalesState) error {
	tag, err := r.db.Exec(ctx, "/* catalog.update_sales_state */ UPDATE events SET sales_state = $2 WHERE id = $1", id, state)
	if err != nil {
		return fmt.Errorf("catalog/pg: update sales state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return api.ErrEventNotFound
	}
	return nil
}
