// Package app — сценарии waiting room.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	catalogapi "github.com/nikolaysavelev/soldout/internal/catalog/api"
	"github.com/nikolaysavelev/soldout/internal/queue/api"
	"github.com/nikolaysavelev/soldout/internal/queue/domain"
)

// ErrNotFound — допуск не найден в хранилище.
var ErrNotFound = errors.New("queue: допуск не найден")

// Store — постоянное хранилище допусков (PostgreSQL, таблица admissions).
type Store interface {
	Insert(ctx context.Context, a domain.Admission) error
	Get(ctx context.Context, token string) (domain.Admission, error)
}

// Cache — быстрый кэш токенов (Valkey) с TTL.
type Cache interface {
	Set(ctx context.Context, a domain.Admission, ttl time.Duration) error
	Get(ctx context.Context, token string) (domain.Admission, error)
}

// Service — реализация api.Service.
type Service struct {
	store   Store
	cache   Cache
	catalog catalogapi.Service
	ttl     time.Duration
	logger  *slog.Logger
	now     func() time.Time
	queue   Queue   // nil — очередь выключена (поведение занятия 1: допуск сразу)
	rate    float64 // ADMISSION_RATE для Stats
}

// New собирает сервис.
func New(store Store, cache Cache, catalog catalogapi.Service, ttl time.Duration, logger *slog.Logger) *Service {
	return &Service{store: store, cache: cache, catalog: catalog, ttl: ttl, logger: logger, now: time.Now}
}

// WithQueue включает настоящую очередь (шаг D, ADR-002): Join ставит в очередь, допуск выдаёт admitter.
func (s *Service) WithQueue(q Queue, rate float64) *Service {
	s.queue = q
	s.rate = rate
	return s
}

var _ api.Service = (*Service)(nil)

// Join — на L1 допуск выдаётся сразу; мероприятие должно существовать.
func (s *Service) Join(ctx context.Context, eventID, userID uuid.UUID) (api.Admission, error) {
	if _, err := s.catalog.GetEvent(ctx, eventID); err != nil {
		return api.Admission{}, err
	}
	if userID == uuid.Nil {
		return api.Admission{}, fmt.Errorf("%w: пустой user_id", api.ErrAdmissionInvalid)
	}
	if s.queue != nil {
		if token, err := s.queue.GetAdmitted(ctx, eventID, userID); err == nil && token != "" {
			return api.Admission{Token: token, EventID: eventID, UserID: userID, Position: 0}, nil
		}
		pos, err := s.queue.Enqueue(ctx, eventID, userID, s.now())
		if err != nil {
			return api.Admission{}, err
		}
		return api.Admission{EventID: eventID, UserID: userID, Position: int(pos), IssuedAt: s.now()}, nil
	}
	a := domain.NewAdmission(eventID, userID, s.now(), s.ttl)
	if err := s.store.Insert(ctx, a); err != nil {
		return api.Admission{}, err
	}
	if err := s.cache.Set(ctx, a, s.ttl); err != nil {
		// кэш — ускорение, не источник истины: деградируем в чтение из PG
		s.logger.WarnContext(ctx, "queue: не удалось записать токен в кэш", "err", err)
	}
	return a.ToAPI(), nil
}

// Validate — сначала кэш, при промахе — PostgreSQL.
func (s *Service) Validate(ctx context.Context, token string, eventID, userID uuid.UUID) error {
	if token == "" {
		return api.ErrAdmissionInvalid
	}
	a, err := s.cache.Get(ctx, token)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.logger.WarnContext(ctx, "queue: ошибка кэша, читаем из БД", "err", err)
		}
		a, err = s.store.Get(ctx, token)
		if errors.Is(err, ErrNotFound) {
			return api.ErrAdmissionInvalid
		}
		if err != nil {
			return err
		}
	}
	if !a.ValidFor(eventID, userID, s.now()) {
		return api.ErrAdmissionInvalid
	}
	return nil
}

// Status — допущен (токен) или позиция в очереди. Только Valkey: p99 < 5 мс.
func (s *Service) Status(ctx context.Context, eventID, userID uuid.UUID) (api.Status, error) {
	if s.queue == nil {
		return api.Status{EventID: eventID, UserID: userID, Admitted: true}, nil
	}
	token, err := s.queue.GetAdmitted(ctx, eventID, userID)
	if err != nil {
		return api.Status{}, err
	}
	if token != "" {
		return api.Status{EventID: eventID, UserID: userID, Admitted: true, Token: token}, nil
	}
	pos, err := s.queue.Position(ctx, eventID, userID)
	if err != nil {
		return api.Status{}, err
	}
	return api.Status{EventID: eventID, UserID: userID, Position: pos}, nil
}

// Stats — размеры очереди (для панели и make queue-status).
func (s *Service) Stats(ctx context.Context, eventID uuid.UUID) (api.Stats, error) {
	st := api.Stats{EventID: eventID, Enabled: s.queue != nil, Rate: s.rate}
	if s.queue == nil {
		return st, nil
	}
	q, inflight, err := s.queue.Sizes(ctx, eventID)
	if err != nil {
		return st, err
	}
	st.Queue, st.Inflight = q, inflight
	return st, nil
}
