// Package pg — PostgreSQL-хранилище booking: holds, orders, order_items.
package pg

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nikolaysavelev/soldout/internal/booking/api"
	"github.com/nikolaysavelev/soldout/internal/booking/app"
	"github.com/nikolaysavelev/soldout/internal/booking/domain"
	"github.com/nikolaysavelev/soldout/internal/platform/db"
	"github.com/nikolaysavelev/soldout/internal/platform/outbox"
)

// Store — реализация app.Store поверх пула.
type Store struct {
	pool *db.Pool
	queries
}

// Option — настройка хранилища.
type Option func(*Store)

// WithOutbox задаёт писателя outbox (режим CDC или polling). По умолчанию — CDC: строка удаляется в той же транзакции.
func WithOutbox(w *outbox.Writer) Option { return func(s *Store) { s.outbox = w } }

// New создаёт хранилище.
func New(pool *db.Pool, opts ...Option) *Store {
	s := &Store{pool: pool, queries: queries{db: pool, outbox: outbox.NewWriter(outbox.Options{DeleteAfterInsert: true})}}
	for _, o := range opts {
		o(s)
	}
	return s
}

// InTx выполняет fn в транзакции.
func (s *Store) InTx(ctx context.Context, fn func(tx app.Tx) error) error {
	return db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&queries{db: tx, outbox: s.outbox})
	})
}

// queries — запросы, одинаковые для пула и транзакции.
type queries struct {
	db     db.DBTX
	outbox *outbox.Writer
}

// AppendOutbox — событие в outbox в текущей транзакции (platform/outbox).
func (q *queries) AppendOutbox(ctx context.Context, e api.OrderPaidEvent) error {
	return q.outbox.Append(ctx, q.db, e)
}

var _ app.Tx = (*queries)(nil)

// LockSeat — advisory-lock на (event, seat) до конца транзакции. Ключ: hashtext(event_id), seat_id.
func (q *queries) LockSeat(ctx context.Context, eventID uuid.UUID, seatID int64) error {
	_, err := q.db.Exec(ctx, "/* booking.lock_seat */ SELECT pg_advisory_xact_lock(hashtext($1::text), $2::int)", eventID, seatID)
	if err != nil {
		return fmt.Errorf("booking/pg: lock seat: %w", err)
	}
	return nil
}

// CountUserHolds — активные и подтверждённые hold пользователя на мероприятие (лимит FR-7).
func (q *queries) CountUserHolds(ctx context.Context, eventID, userID uuid.UUID) (int, error) {
	var n int
	err := q.db.QueryRow(ctx, `/* booking.count_user_holds */
		SELECT count(*) FROM holds WHERE event_id = $1 AND user_id = $2 AND status IN ('active', 'confirmed')`,
		eventID, userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("booking/pg: count user holds: %w", err)
	}
	return n, nil
}

// SeatSold — есть ли подтверждённый hold на место (с шага A — по индексу holds_taken_uidx).
func (q *queries) SeatSold(ctx context.Context, eventID uuid.UUID, seatID int64) (bool, error) {
	var sold bool
	err := q.db.QueryRow(ctx, `/* booking.seat_sold */
		SELECT EXISTS (SELECT 1 FROM holds WHERE event_id = $1 AND seat_id = $2 AND status = 'confirmed')`,
		eventID, seatID).Scan(&sold)
	if err != nil {
		return false, fmt.Errorf("booking/pg: seat sold: %w", err)
	}
	return sold, nil
}

// InsertHold — новый hold; нарушение uniq_active_hold / holds_taken_uidx → ErrSeatHeld.
func (q *queries) InsertHold(ctx context.Context, h domain.Hold) error {
	_, err := q.db.Exec(ctx, `/* booking.insert_hold */
		INSERT INTO holds (id, event_id, seat_id, user_id, status, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		h.ID, h.EventID, h.SeatID, h.UserID, h.Status, h.ExpiresAt, h.CreatedAt)
	if db.IsUniqueViolation(err, "") {
		return api.ErrSeatHeld
	}
	if err != nil {
		return fmt.Errorf("booking/pg: insert hold: %w", err)
	}
	return nil
}

// SectorSeats — все места сектора (V3: поиск смежного отрезка).
func (q *queries) SectorSeats(ctx context.Context, sectorID uuid.UUID) ([]domain.SeatPos, error) {
	rows, err := q.db.Query(ctx, "/* booking.sector_seats */ SELECT id, row_no, seat_no FROM seats WHERE sector_id = $1", sectorID)
	if err != nil {
		return nil, fmt.Errorf("booking/pg: sector seats: %w", err)
	}
	defer rows.Close()
	var out []domain.SeatPos
	for rows.Next() {
		var p domain.SeatPos
		if err := rows.Scan(&p.ID, &p.RowNo, &p.No); err != nil {
			return nil, fmt.Errorf("booking/pg: scan seat: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SectorTakenSeats — занятые места сектора: active и confirmed hold'ы и выпущенные билеты.
func (q *queries) SectorTakenSeats(ctx context.Context, eventID, sectorID uuid.UUID) (map[int64]bool, error) {
	rows, err := q.db.Query(ctx, `/* booking.sector_taken_seats */
		SELECT seat_id FROM holds
		WHERE event_id = $1 AND status IN ('active', 'confirmed') AND seat_id IN (SELECT id FROM seats WHERE sector_id = $2)
		UNION
		SELECT seat_id FROM tickets
		WHERE event_id = $1 AND seat_id IN (SELECT id FROM seats WHERE sector_id = $2)`, eventID, sectorID)
	if err != nil {
		return nil, fmt.Errorf("booking/pg: sector taken seats: %w", err)
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("booking/pg: scan taken seat: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// InsertGroup — запись группы; дубль idempotency_key → ErrIdempotencyConflict.
func (q *queries) InsertGroup(ctx context.Context, g domain.Group) error {
	_, err := q.db.Exec(ctx, `/* booking.insert_group */
		INSERT INTO hold_groups (id, event_id, sector_id, user_id, n, idempotency_key, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, g.ID, g.EventID, g.SectorID, g.UserID, g.N, g.IdempotencyKey, g.CreatedAt)
	if db.IsUniqueViolation(err, "hold_groups_idempotency_key_key") {
		return api.ErrIdempotencyConflict
	}
	if err != nil {
		return fmt.Errorf("booking/pg: insert group: %w", err)
	}
	return nil
}

// InsertHoldsSkipTaken — hold'ы группы одним INSERT; места, уже занятые active или confirmed hold'ом, пропускаются.
func (q *queries) InsertHoldsSkipTaken(ctx context.Context, holds []domain.Hold) (int, error) {
	ids := make([]uuid.UUID, len(holds))
	seats := make([]int64, len(holds))
	for i, h := range holds {
		ids[i], seats[i] = h.ID, h.SeatID
	}
	h := holds[0]
	rows, err := q.db.Query(ctx, `/* booking.insert_group_holds */
		INSERT INTO holds (id, event_id, seat_id, user_id, status, expires_at, created_at, group_id)
		SELECT u.id, $3, u.seat_id, $4, $5, $6, $7, $8 FROM unnest($1::uuid[], $2::bigint[]) AS u(id, seat_id)
		ON CONFLICT DO NOTHING
		RETURNING id`, ids, seats, h.EventID, h.UserID, h.Status, h.ExpiresAt, h.CreatedAt, h.GroupID)
	if err != nil {
		return 0, fmt.Errorf("booking/pg: insert group holds: %w", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("booking/pg: insert group holds: %w", err)
	}
	return n, nil
}

// GroupHoldsForUpdate — hold'ы группы с блокировкой строк (в порядке id).
func (q *queries) GroupHoldsForUpdate(ctx context.Context, groupID uuid.UUID) ([]domain.Hold, error) {
	return q.groupHolds(ctx, "booking.group_holds_for_update", " FOR UPDATE", groupID)
}

func (q *queries) groupHolds(ctx context.Context, name, lock string, groupID uuid.UUID) ([]domain.Hold, error) {
	rows, err := q.db.Query(ctx, "/* "+name+" */ SELECT "+holdCols+" FROM holds WHERE group_id = $1 ORDER BY id"+lock, groupID)
	if err != nil {
		return nil, fmt.Errorf("booking/pg: group holds: %w", err)
	}
	defer rows.Close()
	var out []domain.Hold
	for rows.Next() {
		h, err := scanHold(rows)
		if err != nil {
			return nil, fmt.Errorf("booking/pg: scan hold: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// GroupHoldIDs — id всех hold'ов групп.
func (q *queries) GroupHoldIDs(ctx context.Context, groupIDs []uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.db.Query(ctx, "/* booking.group_hold_ids */ SELECT id FROM holds WHERE group_id = ANY($1) ORDER BY id", groupIDs)
	if err != nil {
		return nil, fmt.Errorf("booking/pg: group hold ids: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("booking/pg: scan hold id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// FindGroupByKey — группа по Idempotency-Key, ее hold'ы и ряд; нет группы → ErrHoldNotFound.
func (q *queries) FindGroupByKey(ctx context.Context, key string) (domain.Group, []domain.Hold, int, error) {
	var g domain.Group
	err := q.db.QueryRow(ctx, `/* booking.get_group_by_key */
		SELECT id, event_id, sector_id, user_id, n, idempotency_key, created_at FROM hold_groups WHERE idempotency_key = $1`, key).
		Scan(&g.ID, &g.EventID, &g.SectorID, &g.UserID, &g.N, &g.IdempotencyKey, &g.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, nil, 0, api.ErrHoldNotFound
	}
	if err != nil {
		return g, nil, 0, fmt.Errorf("booking/pg: get group: %w", err)
	}
	holds, err := q.groupHolds(ctx, "booking.group_holds", "", g.ID)
	if err != nil {
		return g, nil, 0, err
	}
	row := 0
	if len(holds) > 0 {
		if err := q.db.QueryRow(ctx, "/* booking.seat_row */ SELECT row_no FROM seats WHERE id = $1", holds[0].SeatID).Scan(&row); err != nil {
			return g, nil, 0, fmt.Errorf("booking/pg: seat row: %w", err)
		}
	}
	return g, holds, row, nil
}

const holdCols = "id, event_id, seat_id, user_id, status, expires_at, created_at, group_id"

func scanHold(row pgx.Row) (domain.Hold, error) {
	var h domain.Hold
	err := row.Scan(&h.ID, &h.EventID, &h.SeatID, &h.UserID, &h.Status, &h.ExpiresAt, &h.CreatedAt, &h.GroupID)
	return h, err
}

// GetHoldsForUpdate — hold'ы по id с блокировкой строк (в порядке id, чтобы избежать deadlock).
func (q *queries) GetHoldsForUpdate(ctx context.Context, ids []uuid.UUID) ([]domain.Hold, error) {
	rows, err := q.db.Query(ctx, "/* booking.holds_for_update */ SELECT "+holdCols+" FROM holds WHERE id = ANY($1) ORDER BY id FOR UPDATE", ids)
	if err != nil {
		return nil, fmt.Errorf("booking/pg: holds for update: %w", err)
	}
	defer rows.Close()
	var out []domain.Hold
	for rows.Next() {
		h, err := scanHold(rows)
		if err != nil {
			return nil, fmt.Errorf("booking/pg: scan hold: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// UpdateHold — смена статуса.
func (q *queries) UpdateHold(ctx context.Context, h domain.Hold) error {
	tag, err := q.db.Exec(ctx, "/* booking.update_hold */ UPDATE holds SET status = $2, expires_at = $3 WHERE id = $1", h.ID, h.Status, h.ExpiresAt)
	if err != nil {
		return fmt.Errorf("booking/pg: update hold: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return api.ErrHoldNotFound
	}
	return nil
}

// HoldsInOpenOrders — входит ли хоть один hold в pending/paid заказ.
func (q *queries) HoldsInOpenOrders(ctx context.Context, ids []uuid.UUID) (bool, error) {
	var busy bool
	err := q.db.QueryRow(ctx, `/* booking.holds_in_open_orders */
		SELECT EXISTS (
			SELECT 1 FROM order_items oi JOIN orders o ON o.id = oi.order_id
			WHERE oi.hold_id = ANY($1) AND o.status IN ('pending', 'paid'))`, ids).Scan(&busy)
	if err != nil {
		return false, fmt.Errorf("booking/pg: holds in open orders: %w", err)
	}
	return busy, nil
}

// InsertOrder — заказ и позиции; дубль idempotency_key → ErrIdempotencyConflict.
func (q *queries) InsertOrder(ctx context.Context, o domain.Order) error {
	_, err := q.db.Exec(ctx, `/* booking.insert_order */
		INSERT INTO orders (id, event_id, user_id, status, amount, idempotency_key, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8)`,
		o.ID, o.EventID, o.UserID, o.Status, o.AmountString(), o.IdempotencyKey, o.CreatedAt, o.UpdatedAt)
	if db.IsUniqueViolation(err, "orders_idempotency_key_key") {
		return api.ErrIdempotencyConflict
	}
	if err != nil {
		return fmt.Errorf("booking/pg: insert order: %w", err)
	}
	for _, hid := range o.HoldIDs {
		if _, err := q.db.Exec(ctx, "/* booking.insert_order_item */ INSERT INTO order_items (order_id, hold_id) VALUES ($1, $2)", o.ID, hid); err != nil {
			return fmt.Errorf("booking/pg: insert order item: %w", err)
		}
	}
	return nil
}

const orderSelect = `SELECT o.id, o.event_id, o.user_id, o.status, o.amount::text, o.idempotency_key, o.created_at, o.updated_at,
		coalesce(array_agg(oi.hold_id ORDER BY h.id) FILTER (WHERE oi.hold_id IS NOT NULL), '{}'),
		coalesce(array_agg(h.seat_id ORDER BY h.id) FILTER (WHERE h.seat_id IS NOT NULL), '{}')
	FROM orders o
	LEFT JOIN order_items oi ON oi.order_id = o.id
	LEFT JOIN holds h ON h.id = oi.hold_id`

func scanOrder(row pgx.Row) (domain.Order, error) {
	var o domain.Order
	var amount string
	err := row.Scan(&o.ID, &o.EventID, &o.UserID, &o.Status, &amount, &o.IdempotencyKey, &o.CreatedAt, &o.UpdatedAt, &o.HoldIDs, &o.SeatIDs)
	if err != nil {
		return o, err
	}
	o.AmountMinor, err = parseMinor(amount)
	return o, err
}

func parseMinor(amount string) (int64, error) {
	whole, frac, _ := strings.Cut(amount, ".")
	frac = (frac + "00")[:2]
	n, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("booking/pg: amount %q: %w", amount, err)
	}
	return n, nil
}

func (q *queries) getOrder(ctx context.Context, name, where, lock string, arg any) (domain.Order, error) {
	sql := fmt.Sprintf("/* %s */ %s WHERE %s GROUP BY o.id %s", name, orderSelect, where, lock)
	o, err := scanOrder(q.db.QueryRow(ctx, sql, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return o, api.ErrOrderNotFound
	}
	if err != nil {
		return o, fmt.Errorf("booking/pg: get order: %w", err)
	}
	return o, nil
}

// GetOrderForUpdate — заказ с блокировкой строки (FOR UPDATE OF o).
func (q *queries) GetOrderForUpdate(ctx context.Context, id uuid.UUID) (domain.Order, error) {
	// FOR UPDATE несовместим с GROUP BY, поэтому блокируем отдельным запросом.
	if _, err := q.db.Exec(ctx, "/* booking.lock_order */ SELECT id FROM orders WHERE id = $1 FOR UPDATE", id); err != nil {
		return domain.Order{}, fmt.Errorf("booking/pg: lock order: %w", err)
	}
	return q.getOrder(ctx, "booking.get_order_locked", "o.id = $1", "", id)
}

// UpdateOrder — смена статуса.
func (q *queries) UpdateOrder(ctx context.Context, o domain.Order) error {
	tag, err := q.db.Exec(ctx, "/* booking.update_order */ UPDATE orders SET status = $2, updated_at = $3 WHERE id = $1", o.ID, o.Status, o.UpdatedAt)
	if err != nil {
		return fmt.Errorf("booking/pg: update order: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return api.ErrOrderNotFound
	}
	return nil
}

// GetHold — чтение без блокировки.
func (q *queries) GetHold(ctx context.Context, id uuid.UUID) (domain.Hold, error) {
	h, err := scanHold(q.db.QueryRow(ctx, "/* booking.get_hold */ SELECT "+holdCols+" FROM holds WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return h, api.ErrHoldNotFound
	}
	if err != nil {
		return h, fmt.Errorf("booking/pg: get hold: %w", err)
	}
	return h, nil
}

// GetOrder — чтение без блокировки.
func (q *queries) GetOrder(ctx context.Context, id uuid.UUID) (domain.Order, error) {
	return q.getOrder(ctx, "booking.get_order", "o.id = $1", "", id)
}

// FindOrderByIdempotencyKey — заказ по ключу идемпотентности.
func (q *queries) FindOrderByIdempotencyKey(ctx context.Context, key string) (domain.Order, error) {
	return q.getOrder(ctx, "booking.get_order_by_key", "o.idempotency_key = $1", "", key)
}

// ListUserOrders — заказы пользователя (новые первыми).
func (q *queries) ListUserOrders(ctx context.Context, userID uuid.UUID) ([]domain.Order, error) {
	rows, err := q.db.Query(ctx, "/* booking.list_user_orders */ "+orderSelect+" WHERE o.user_id = $1 GROUP BY o.id ORDER BY o.created_at DESC", userID)
	if err != nil {
		return nil, fmt.Errorf("booking/pg: list user orders: %w", err)
	}
	defer rows.Close()
	var out []domain.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("booking/pg: scan order: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SeatStates — held/sold по мероприятию. Индекса по (event_id, status) нет намеренно (ADR-001).
func (q *queries) SeatStates(ctx context.Context, eventID uuid.UUID) (map[int64]api.SeatState, error) {
	rows, err := q.db.Query(ctx, `/* booking.seat_states */
		SELECT seat_id, status FROM holds WHERE event_id = $1 AND status IN ('active', 'confirmed')`, eventID)
	if err != nil {
		return nil, fmt.Errorf("booking/pg: seat states: %w", err)
	}
	defer rows.Close()
	out := map[int64]api.SeatState{}
	for rows.Next() {
		var seatID int64
		var status api.HoldStatus
		if err := rows.Scan(&seatID, &status); err != nil {
			return nil, fmt.Errorf("booking/pg: scan seat state: %w", err)
		}
		if status == api.HoldConfirmed {
			out[seatID] = api.SeatStateSold
		} else if _, sold := out[seatID]; !sold {
			out[seatID] = api.SeatStateHeld
		}
	}
	return out, rows.Err()
}

// SoldCount — подтверждённые hold мероприятия.
func (q *queries) SoldCount(ctx context.Context, eventID uuid.UUID) (int, error) {
	var n int
	if err := q.db.QueryRow(ctx, "/* booking.sold_count */ SELECT count(*) FROM holds WHERE event_id = $1 AND status = 'confirmed'", eventID).Scan(&n); err != nil {
		return 0, fmt.Errorf("booking/pg: sold count: %w", err)
	}
	return n, nil
}

// ExpireHoldsBatch — до limit просроченных active hold → released одним UPDATE; конкурентные
// транзакции (pay, release) не блокируются: строки берутся FOR UPDATE SKIP LOCKED.
// V3: батч дополняется остальными active hold'ами тех же групп — группа истекает целиком.
func (q *queries) ExpireHoldsBatch(ctx context.Context, now time.Time, limit int) ([]app.ReleasedHold, error) {
	rows, err := q.db.Query(ctx, `/* booking.expire_holds_batch */
		WITH batch AS (
			SELECT id, group_id FROM holds WHERE status = 'active' AND expires_at <= $1
			ORDER BY expires_at LIMIT $2 FOR UPDATE SKIP LOCKED),
		target AS (
			SELECT id FROM batch
			UNION
			SELECT id FROM holds WHERE status = 'active' AND group_id IN (SELECT group_id FROM batch WHERE group_id IS NOT NULL))
		UPDATE holds h SET status = 'released' FROM target WHERE h.id = target.id AND h.status = 'active'
		RETURNING h.id, h.event_id, h.seat_id`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("booking/pg: expire holds: %w", err)
	}
	defer rows.Close()
	var out []app.ReleasedHold
	for rows.Next() {
		var r app.ReleasedHold
		if err := rows.Scan(&r.ID, &r.EventID, &r.SeatID); err != nil {
			return nil, fmt.Errorf("booking/pg: scan released: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ExpireOrders — pending заказы, у которых хотя бы один hold released → expired.
func (q *queries) ExpireOrders(ctx context.Context, now time.Time) (int64, error) {
	tag, err := q.db.Exec(ctx, `/* booking.expire_orders */
		UPDATE orders SET status = 'expired', updated_at = $1
		WHERE status = 'pending' AND EXISTS (
			SELECT 1 FROM order_items oi JOIN holds h ON h.id = oi.hold_id
			WHERE oi.order_id = orders.id AND h.status = 'released')`, now)
	if err != nil {
		return 0, fmt.Errorf("booking/pg: expire orders: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CountActiveHolds — для gauge holds_active.
func (q *queries) CountActiveHolds(ctx context.Context) (int64, error) {
	var n int64
	if err := q.db.QueryRow(ctx, "/* booking.count_active_holds */ SELECT count(*) FROM holds WHERE status = 'active'").Scan(&n); err != nil {
		return 0, fmt.Errorf("booking/pg: count active holds: %w", err)
	}
	return n, nil
}
