// Package app — сценарии бронирования: CreateHold, ReleaseHold, CreateOrder, PayOrder, expirer.
//
// Транзакционные границы: каждая мутация — одна транзакция PostgreSQL через Store.InTx.
// Блокировка места — advisory-lock на (event_id, seat_id): booking не трогает таблицу seats (она принадлежит catalog).
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/booking/api"
	"github.com/nikolaysavelev/soldout/internal/booking/domain"
	catalogapi "github.com/nikolaysavelev/soldout/internal/catalog/api"
	"github.com/nikolaysavelev/soldout/internal/platform/eventbus"
	"github.com/nikolaysavelev/soldout/internal/platform/faults"
	queueapi "github.com/nikolaysavelev/soldout/internal/queue/api"
)

// Tx — операции внутри транзакции.
type Tx interface {
	CountUserHolds(ctx context.Context, eventID, userID uuid.UUID) (int, error)
	SeatSold(ctx context.Context, eventID uuid.UUID, seatID int64) (bool, error)
	// InsertHold — INSERT … ON CONFLICT DO NOTHING RETURNING по holds_taken_uidx: false = место занято/продано.
	InsertHold(ctx context.Context, h domain.Hold) (bool, error)
	TakenSeats(ctx context.Context, eventID uuid.UUID, seatIDs []int64) (map[int64]bool, error)
	ClaimIdempotencyKey(ctx context.Context, userID uuid.UUID, key, requestHash string) (claimed bool, existing IdempotencyRecord, err error)
	BindIdempotencyKey(ctx context.Context, userID uuid.UUID, key string, holdID uuid.UUID) error
	GetHoldsForUpdate(ctx context.Context, ids []uuid.UUID) ([]domain.Hold, error)
	UpdateHold(ctx context.Context, h domain.Hold) error
	HoldsInOpenOrders(ctx context.Context, ids []uuid.UUID) (bool, error)
	InsertOrder(ctx context.Context, o domain.Order) error
	GetOrderForUpdate(ctx context.Context, id uuid.UUID) (domain.Order, error)
	UpdateOrder(ctx context.Context, o domain.Order) error
}

// Store — хранилище booking (реализация в adapters/pg).
type Store interface {
	InTx(ctx context.Context, fn func(tx Tx) error) error
	GetHold(ctx context.Context, id uuid.UUID) (domain.Hold, error)
	GetOrder(ctx context.Context, id uuid.UUID) (domain.Order, error)
	FindOrderByIdempotencyKey(ctx context.Context, key string) (domain.Order, error)
	ListUserOrders(ctx context.Context, userID uuid.UUID) ([]domain.Order, error)
	SeatStates(ctx context.Context, eventID uuid.UUID) (map[int64]api.SeatState, error)
	SoldCount(ctx context.Context, eventID uuid.UUID) (int, error)
	ExpireHoldsBatch(ctx context.Context, now time.Time, limit int) ([]ReleasedHold, error)
	ExpireOrders(ctx context.Context, now time.Time) (int64, error)
	CountActiveHolds(ctx context.Context) (int64, error)
}

// IdempotencyRecord — сохранённый результат запроса с Idempotency-Key.
type IdempotencyRecord struct {
	RequestHash string
	HoldID      uuid.UUID // Nil — запрос ещё выполняется (in-flight)
}

// Contention — счётчик исходов hold (metrics.HoldContention).
type Contention interface{ Inc(outcome string) }

// ReleasedHold — hold, освобождённый expirer'ом (для событий карты зала).
type ReleasedHold struct {
	ID      uuid.UUID
	EventID uuid.UUID
	SeatID  int64
}

// Options — настройки сервиса.
type Options struct {
	HoldTTL          time.Duration
	TicketPriceMinor int64
	Faults           faults.Flags
	Events           eventbus.Publisher // события SeatStateChanged после commit; nil = не публиковать
	Contention       Contention         // hold_contention_total{outcome}; nil = не считать
	AnyCandidates    int                // сколько кандидатов перебирает /holds/any до sector_exhausted (по умолчанию 8)
}

// Service — реализация api.Service.
type Service struct {
	store   Store
	catalog catalogapi.Service
	queue   queueapi.Service
	payment api.PaymentGateway
	tickets api.TicketIssuer
	opts    Options
	logger  *slog.Logger
	now     func() time.Time
}

// New собирает сервис.
func New(store Store, catalog catalogapi.Service, queue queueapi.Service, payment api.PaymentGateway, tickets api.TicketIssuer, opts Options, logger *slog.Logger) *Service {
	if opts.HoldTTL <= 0 {
		opts.HoldTTL = 10 * time.Minute
	}
	if opts.Events == nil {
		opts.Events = eventbus.Nop{}
	}
	if opts.Contention == nil {
		opts.Contention = nopContention{}
	}
	if opts.AnyCandidates <= 0 {
		opts.AnyCandidates = 8
	}
	return &Service{store: store, catalog: catalog, queue: queue, payment: payment, tickets: tickets, opts: opts, logger: logger, now: time.Now}
}

var _ api.Service = (*Service)(nil)

type nopContention struct{}

func (nopContention) Inc(string) {}

// CreateHold — FR-3/FR-7: удержать место на HoldTTL; 409 если занято/продано, 422 если лимит.
// Без блокировок: гонку за место закрывает partial unique index holds_taken_uidx (ON CONFLICT DO NOTHING) —
// конфликт мгновенный, очереди нет. Advisory lock занятия 1 дублировал индекс и лишь строил очередь.
func (s *Service) CreateHold(ctx context.Context, in api.CreateHoldInput) (api.Hold, error) {
	if in.EventID == uuid.Nil || in.UserID == uuid.Nil || in.SeatID <= 0 {
		return api.Hold{}, fmt.Errorf("%w: event_id, seat_id и user_id обязательны", api.ErrValidation)
	}
	ev, err := s.catalog.GetEvent(ctx, in.EventID)
	if err != nil {
		return api.Hold{}, err
	}
	if ev.SalesState != catalogapi.SalesOpen {
		return api.Hold{}, fmt.Errorf("%w: состояние %s", api.ErrSalesClosed, ev.SalesState)
	}
	seat, err := s.catalog.GetSeat(ctx, in.SeatID)
	if err != nil {
		return api.Hold{}, err
	}
	if seat.VenueID != ev.VenueID {
		return api.Hold{}, api.ErrSeatNotInEvent
	}
	if err := s.queue.Validate(ctx, in.AdmissionToken, in.EventID, in.UserID); err != nil {
		if errors.Is(err, queueapi.ErrAdmissionInvalid) {
			return api.Hold{}, fmt.Errorf("%w: %v", api.ErrAdmissionRequired, err)
		}
		return api.Hold{}, err
	}

	hold := domain.NewHold(in.EventID, in.SeatID, in.UserID, s.now(), s.opts.HoldTTL)
	err = s.store.InTx(ctx, func(tx Tx) error {
		n, err := tx.CountUserHolds(ctx, in.EventID, in.UserID) // индекс holds_user_active_idx
		if err != nil {
			return err
		}
		if !domain.CanHoldMore(n) {
			s.opts.Contention.Inc("limit")
			return fmt.Errorf("%w: уже %d", api.ErrHoldLimit, n)
		}
		inserted, err := tx.InsertHold(ctx, hold)
		if err != nil {
			return err
		}
		if !inserted {
			sold, err := tx.SeatSold(ctx, in.EventID, in.SeatID) // индекс holds_taken_uidx
			if err != nil {
				return err
			}
			if sold {
				s.opts.Contention.Inc("seat_sold")
				return api.ErrSeatSold
			}
			s.opts.Contention.Inc("seat_held")
			return api.ErrSeatHeld
		}
		return nil
	})
	if err != nil {
		return api.Hold{}, err
	}
	s.opts.Contention.Inc("created")
	s.publishSeat(ctx, hold.EventID, hold.SeatID, api.SeatStateHeld, hold.ID)
	return hold.ToAPI(), nil
}

// CreateAnyHold — «любое свободное место в секторе» с обязательным Idempotency-Key.
// Кандидаты — места сектора минус занятые; старт с случайной позиции распределяет конкурентов,
// гонку закрывает индекс (ON CONFLICT DO NOTHING). Ключ идемпотентности захватывается в той же транзакции.
func (s *Service) CreateAnyHold(ctx context.Context, in api.CreateAnyHoldInput) (api.Hold, bool, error) {
	if in.EventID == uuid.Nil || in.SectorID == uuid.Nil || in.UserID == uuid.Nil {
		return api.Hold{}, false, fmt.Errorf("%w: event_id, sector_id и user_id обязательны", api.ErrValidation)
	}
	if in.IdempotencyKey == "" {
		return api.Hold{}, false, fmt.Errorf("%w: заголовок Idempotency-Key обязателен", api.ErrValidation)
	}
	ev, err := s.catalog.GetEvent(ctx, in.EventID)
	if err != nil {
		return api.Hold{}, false, err
	}
	if ev.SalesState != catalogapi.SalesOpen {
		return api.Hold{}, false, fmt.Errorf("%w: состояние %s", api.ErrSalesClosed, ev.SalesState)
	}
	sec, err := s.catalog.GetSector(ctx, in.SectorID)
	if err != nil {
		return api.Hold{}, false, err
	}
	if sec.VenueID != ev.VenueID {
		return api.Hold{}, false, api.ErrSeatNotInEvent
	}
	if err := s.queue.Validate(ctx, in.AdmissionToken, in.EventID, in.UserID); err != nil {
		if errors.Is(err, queueapi.ErrAdmissionInvalid) {
			return api.Hold{}, false, fmt.Errorf("%w: %v", api.ErrAdmissionRequired, err)
		}
		return api.Hold{}, false, err
	}
	seatIDs, err := s.catalog.SectorSeatIDs(ctx, in.SectorID)
	if err != nil {
		return api.Hold{}, false, err
	}

	var hold domain.Hold
	var replay *api.Hold
	err = s.store.InTx(ctx, func(tx Tx) error {
		claimed, existing, err := tx.ClaimIdempotencyKey(ctx, in.UserID, in.IdempotencyKey, in.RequestHash)
		if err != nil {
			return err
		}
		if !claimed {
			if existing.RequestHash != in.RequestHash {
				s.opts.Contention.Inc("idempotency_mismatch")
				return api.ErrIdempotencyConflict
			}
			if existing.HoldID == uuid.Nil {
				return api.ErrIdempotencyInFlight
			}
			h, err := s.store.GetHold(ctx, existing.HoldID)
			if err != nil {
				return err
			}
			dto := h.ToAPI()
			replay = &dto
			s.opts.Contention.Inc("idempotent_replay")
			return nil
		}
		n, err := tx.CountUserHolds(ctx, in.EventID, in.UserID)
		if err != nil {
			return err
		}
		if !domain.CanHoldMore(n) {
			s.opts.Contention.Inc("limit")
			return fmt.Errorf("%w: уже %d", api.ErrHoldLimit, n)
		}
		taken, err := tx.TakenSeats(ctx, in.EventID, seatIDs)
		if err != nil {
			return err
		}
		free := make([]int64, 0, len(seatIDs))
		for _, id := range seatIDs {
			if !taken[id] {
				free = append(free, id)
			}
		}
		if len(free) == 0 {
			s.opts.Contention.Inc("sector_exhausted")
			return api.ErrSectorExhausted
		}
		start := rand.IntN(len(free))
		for i := 0; i < len(free) && i < s.opts.AnyCandidates; i++ {
			seatID := free[(start+i)%len(free)]
			hold = domain.NewHold(in.EventID, seatID, in.UserID, s.now(), s.opts.HoldTTL)
			inserted, err := tx.InsertHold(ctx, hold)
			if err != nil {
				return err
			}
			if inserted {
				return tx.BindIdempotencyKey(ctx, in.UserID, in.IdempotencyKey, hold.ID)
			}
		}
		s.opts.Contention.Inc("sector_exhausted")
		return api.ErrSectorExhausted
	})
	if err != nil {
		return api.Hold{}, false, err
	}
	if replay != nil {
		return *replay, false, nil
	}
	s.opts.Contention.Inc("created")
	s.publishSeat(ctx, hold.EventID, hold.SeatID, api.SeatStateHeld, hold.ID)
	return hold.ToAPI(), true, nil
}

// publishSeat публикует SeatStateChanged после commit (карта зала — проекция в catalog).
func (s *Service) publishSeat(ctx context.Context, eventID uuid.UUID, seatID int64, state api.SeatState, holdID uuid.UUID) {
	_ = s.opts.Events.Publish(ctx, api.SeatStateChanged{EventID: eventID, SeatID: seatID, State: state, HoldID: holdID, At: s.now()})
}

// ReleaseHold — DELETE /v1/holds/{id}.
func (s *Service) ReleaseHold(ctx context.Context, holdID uuid.UUID) error {
	var released domain.Hold
	err := s.store.InTx(ctx, func(tx Tx) error {
		holds, err := tx.GetHoldsForUpdate(ctx, []uuid.UUID{holdID})
		if err != nil {
			return err
		}
		if len(holds) != 1 {
			return api.ErrHoldNotFound
		}
		h := holds[0]
		if err := h.Release(); err != nil {
			return err
		}
		released = h
		return tx.UpdateHold(ctx, h)
	})
	if err != nil {
		return err
	}
	s.publishSeat(ctx, released.EventID, released.SeatID, api.SeatStateFree, released.ID)
	return nil
}

// GetHold — чтение.
func (s *Service) GetHold(ctx context.Context, holdID uuid.UUID) (api.Hold, error) {
	h, err := s.store.GetHold(ctx, holdID)
	if err != nil {
		return api.Hold{}, err
	}
	return h.ToAPI(), nil
}

// CreateOrder — POST /v1/orders с Idempotency-Key. Повтор с тем же ключом возвращает существующий заказ.
func (s *Service) CreateOrder(ctx context.Context, in api.CreateOrderInput) (api.Order, bool, error) {
	if in.IdempotencyKey == "" {
		return api.Order{}, false, fmt.Errorf("%w: заголовок Idempotency-Key обязателен", api.ErrValidation)
	}
	if in.UserID == uuid.Nil || len(in.HoldIDs) == 0 {
		return api.Order{}, false, fmt.Errorf("%w: user_id и hold_ids обязательны", api.ErrValidation)
	}
	if existing, err := s.store.FindOrderByIdempotencyKey(ctx, in.IdempotencyKey); err == nil {
		if existing.UserID != in.UserID || !sameIDs(existing.HoldIDs, in.HoldIDs) {
			return api.Order{}, false, api.ErrIdempotencyConflict
		}
		return existing.ToAPI(), false, nil
	} else if !errors.Is(err, api.ErrOrderNotFound) {
		return api.Order{}, false, err
	}

	var order domain.Order
	err := s.store.InTx(ctx, func(tx Tx) error {
		holds, err := tx.GetHoldsForUpdate(ctx, in.HoldIDs)
		if err != nil {
			return err
		}
		if len(holds) != len(uniqueIDs(in.HoldIDs)) {
			return api.ErrHoldNotFound
		}
		order, err = domain.NewOrder(holds, in.UserID, in.IdempotencyKey, s.opts.TicketPriceMinor, s.now())
		if err != nil {
			return err
		}
		busy, err := tx.HoldsInOpenOrders(ctx, order.HoldIDs)
		if err != nil {
			return err
		}
		if busy {
			return api.ErrHoldAlreadyOrdered
		}
		return tx.InsertOrder(ctx, order)
	})
	if errors.Is(err, api.ErrIdempotencyConflict) {
		// гонка двух одинаковых запросов: второй получает уже созданный заказ
		if existing, err2 := s.store.FindOrderByIdempotencyKey(ctx, in.IdempotencyKey); err2 == nil && existing.UserID == in.UserID {
			return existing.ToAPI(), false, nil
		}
	}
	if err != nil {
		return api.Order{}, false, err
	}
	return order.ToAPI(), true, nil
}

// PayOrder — POST /v1/orders/{id}/pay: платёж → confirm hold'ов → выпуск билетов (синхронно на L1).
func (s *Service) PayOrder(ctx context.Context, in api.PayOrderInput) (api.Order, error) {
	order, err := s.store.GetOrder(ctx, in.OrderID)
	if err != nil {
		return api.Order{}, err
	}
	switch order.Status {
	case api.OrderPaid:
		// идемпотентный повтор: убеждаемся, что билеты выпущены (Issue идемпотентен)
		return order.ToAPI(), s.issueTickets(ctx, order)
	case api.OrderFailed, api.OrderExpired:
		return api.Order{}, fmt.Errorf("%w: статус %s", api.ErrOrderNotPending, order.Status)
	}

	// 1. hold'ы ещё живы? иначе заказ истекает
	now := s.now()
	err = s.store.InTx(ctx, func(tx Tx) error {
		o, err := tx.GetOrderForUpdate(ctx, in.OrderID)
		if err != nil {
			return err
		}
		if o.Status != api.OrderPending {
			return nil
		}
		holds, err := tx.GetHoldsForUpdate(ctx, o.HoldIDs)
		if err != nil {
			return err
		}
		for _, h := range holds {
			if !h.IsUsable(now) {
				if err := o.MarkExpired(now); err != nil {
					return err
				}
				if err := tx.UpdateOrder(ctx, o); err != nil {
					return err
				}
				return api.ErrOrderExpired
			}
		}
		return nil
	})
	if err != nil {
		return api.Order{}, err
	}

	// 2. платёж (идемпотентен по ключу; повтор с тем же ключом не списывает второй раз)
	key := in.IdempotencyKey
	if key == "" {
		key = "order:" + order.ID.String()
	}
	res, err := s.payment.Charge(ctx, api.ChargeRequest{OrderID: order.ID, UserID: order.UserID, AmountMinor: order.AmountMinor, IdempotencyKey: key})
	if err != nil {
		return api.Order{}, fmt.Errorf("booking: платёжный шлюз: %w", err)
	}
	now = s.now()
	if !res.Succeeded {
		// FR-4: при неудаче hold снимается
		var freed []domain.Hold
		err = s.store.InTx(ctx, func(tx Tx) error {
			o, err := tx.GetOrderForUpdate(ctx, order.ID)
			if err != nil {
				return err
			}
			if o.Status != api.OrderPending {
				return nil
			}
			if err := o.MarkFailed(now); err != nil {
				return err
			}
			if err := tx.UpdateOrder(ctx, o); err != nil {
				return err
			}
			holds, err := tx.GetHoldsForUpdate(ctx, o.HoldIDs)
			if err != nil {
				return err
			}
			for i := range holds {
				if holds[i].Status == api.HoldActive {
					if err := holds[i].Release(); err != nil {
						return err
					}
					if err := tx.UpdateHold(ctx, holds[i]); err != nil {
						return err
					}
					freed = append(freed, holds[i])
				}
			}
			return nil
		})
		if err != nil {
			return api.Order{}, err
		}
		for _, h := range freed {
			s.publishSeat(ctx, h.EventID, h.SeatID, api.SeatStateFree, h.ID)
		}
		return api.Order{}, fmt.Errorf("%w: %s", api.ErrPaymentFailed, res.Reason)
	}

	// 3. подтверждаем hold'ы и заказ в одной транзакции
	var paid domain.Order
	var confirmed []domain.Hold
	err = s.store.InTx(ctx, func(tx Tx) error {
		o, err := tx.GetOrderForUpdate(ctx, order.ID)
		if err != nil {
			return err
		}
		if o.Status == api.OrderPaid { // гонка двух pay
			paid = o
			return nil
		}
		holds, err := tx.GetHoldsForUpdate(ctx, o.HoldIDs)
		if err != nil {
			return err
		}
		for i := range holds {
			if err := holds[i].Confirm(now); err != nil {
				// деньги списаны, место потеряно: на L1 фиксируем failed и пишем в лог; компенсация — занятие 7
				s.logger.ErrorContext(ctx, "оплата прошла, но hold нельзя подтвердить — нужна компенсация", "order_id", o.ID, "hold_id", holds[i].ID, "err", err)
				if err := o.MarkFailed(now); err != nil {
					return err
				}
				if err := tx.UpdateOrder(ctx, o); err != nil {
					return err
				}
				return fmt.Errorf("%w: %v", api.ErrOrderExpired, err)
			}
			if err := tx.UpdateHold(ctx, holds[i]); err != nil {
				return err
			}
			confirmed = append(confirmed, holds[i])
		}
		if err := o.MarkPaid(now); err != nil {
			return err
		}
		if err := tx.UpdateOrder(ctx, o); err != nil {
			return err
		}
		paid = o
		return nil
	})
	if err != nil {
		return api.Order{}, err
	}
	for _, h := range confirmed {
		s.publishSeat(ctx, h.EventID, h.SeatID, api.SeatStateSold, h.ID)
	}
	s.opts.Faults.MaybeCrashAfterCommit(s.logger, "order.paid")

	// 4. выпуск билетов и уведомление — синхронно (L1); с занятия 3 — через outbox
	if err := s.issueTickets(ctx, paid); err != nil {
		return paid.ToAPI(), err
	}
	return paid.ToAPI(), nil
}

func (s *Service) issueTickets(ctx context.Context, o domain.Order) error {
	err := s.tickets.Issue(ctx, api.IssueRequest{OrderID: o.ID, EventID: o.EventID, UserID: o.UserID, SeatIDs: o.SeatIDs})
	if err != nil {
		return fmt.Errorf("booking: выпуск билетов для заказа %s: %w", o.ID, err)
	}
	return nil
}

// GetOrder — чтение.
func (s *Service) GetOrder(ctx context.Context, orderID uuid.UUID) (api.Order, error) {
	o, err := s.store.GetOrder(ctx, orderID)
	if err != nil {
		return api.Order{}, err
	}
	return o.ToAPI(), nil
}

// ListUserOrders — заказы пользователя (для ticketing).
func (s *Service) ListUserOrders(ctx context.Context, userID uuid.UUID) ([]api.Order, error) {
	orders, err := s.store.ListUserOrders(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]api.Order, 0, len(orders))
	for _, o := range orders {
		out = append(out, o.ToAPI())
	}
	return out, nil
}

// SeatStates — held/sold по местам мероприятия.
func (s *Service) SeatStates(ctx context.Context, eventID uuid.UUID) (map[int64]api.SeatState, error) {
	return s.store.SeatStates(ctx, eventID)
}

// SoldCount — число проданных мест мероприятия (confirmed hold).
func (s *Service) SoldCount(ctx context.Context, eventID uuid.UUID) (int, error) {
	return s.store.SoldCount(ctx, eventID)
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := ids[:0:0]
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func sameIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[uuid.UUID]struct{}, len(a))
	for _, id := range a {
		set[id] = struct{}{}
	}
	for _, id := range b {
		if _, ok := set[id]; !ok {
			return false
		}
	}
	return true
}
