// Package http — /v1/holds, /v1/orders.
package http

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/booking/api"
	catalogapi "github.com/nikolaysavelev/soldout/internal/catalog/api"
	"github.com/nikolaysavelev/soldout/internal/platform/httpx"
)

// Handler — хендлеры бронирования.
type Handler struct{ svc api.Service }

// New создаёт хендлеры.
func New(svc api.Service) *Handler { return &Handler{svc: svc} }

// Mount регистрирует маршруты.
func (h *Handler) Mount(r chi.Router) {
	r.Post("/v1/holds", h.createHold)
	r.Post("/v1/holds/any", h.createAnyHold)
	r.Get("/v1/holds/{id}", h.getHold)
	r.Delete("/v1/holds/{id}", h.releaseHold)
	r.Post("/v1/orders", h.createOrder)
	r.Get("/v1/orders/{id}", h.getOrder)
	r.Post("/v1/orders/{id}/pay", h.payOrder)
}

type holdRequest struct {
	EventID uuid.UUID `json:"event_id"`
	SeatID  int64     `json:"seat_id"`
	UserID  uuid.UUID `json:"user_id"`
}

func (h *Handler) createHold(w http.ResponseWriter, r *http.Request) {
	var in holdRequest
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	hold, err := h.svc.CreateHold(r.Context(), api.CreateHoldInput{
		EventID: in.EventID, SeatID: in.SeatID, UserID: in.UserID, AdmissionToken: r.Header.Get("X-Admission-Token"),
	})
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	httpx.JSON(w, http.StatusCreated, hold)
}

type anyHoldRequest struct {
	EventID  uuid.UUID `json:"event_id"`
	SectorID uuid.UUID `json:"sector_id"`
	UserID   uuid.UUID `json:"user_id"`
}

// createAnyHold — «любое место в секторе»; Idempotency-Key обязателен, тело хэшируется для сравнения повторов.
func (h *Handler) createAnyHold(w http.ResponseWriter, r *http.Request) {
	var in anyHoldRequest
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "idempotency_key_required", "заголовок Idempotency-Key обязателен"))
		return
	}
	sum := sha256.Sum256([]byte(in.EventID.String() + "|" + in.SectorID.String() + "|" + in.UserID.String()))
	hold, created, err := h.svc.CreateAnyHold(r.Context(), api.CreateAnyHoldInput{
		EventID: in.EventID, SectorID: in.SectorID, UserID: in.UserID, AdmissionToken: r.Header.Get("X-Admission-Token"),
		IdempotencyKey: key, RequestHash: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	httpx.JSON(w, status, hold)
}

func (h *Handler) getHold(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	hold, err := h.svc.GetHold(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, hold)
}

func (h *Handler) releaseHold(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.svc.ReleaseHold(r.Context(), id); err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type orderRequest struct {
	HoldIDs []uuid.UUID `json:"hold_ids"`
	UserID  uuid.UUID   `json:"user_id"`
}

func (h *Handler) createOrder(w http.ResponseWriter, r *http.Request) {
	var in orderRequest
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	order, created, err := h.svc.CreateOrder(r.Context(), api.CreateOrderInput{
		HoldIDs: in.HoldIDs, UserID: in.UserID, IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	httpx.JSON(w, status, order)
}

func (h *Handler) getOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	order, err := h.svc.GetOrder(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, order)
}

func (h *Handler) payOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	order, err := h.svc.PayOrder(r.Context(), api.PayOrderInput{OrderID: id, IdempotencyKey: r.Header.Get("Idempotency-Key")})
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, order)
}

func parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "bad_request", "id должен быть UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, api.ErrSeatHeld):
		return httpx.Wrap(http.StatusConflict, "seat_held", err)
	case errors.Is(err, api.ErrSeatSold):
		return httpx.Wrap(http.StatusConflict, "seat_sold", err)
	case errors.Is(err, api.ErrSectorExhausted):
		return httpx.Wrap(http.StatusConflict, "sector_exhausted", err)
	case errors.Is(err, api.ErrIdempotencyInFlight):
		return httpx.Wrap(http.StatusConflict, "idempotency_in_flight", err)
	case errors.Is(err, catalogapi.ErrSectorNotFound):
		return httpx.Wrap(http.StatusNotFound, "not_found", err)
	case errors.Is(err, api.ErrHoldLimit):
		return httpx.Wrap(http.StatusUnprocessableEntity, "hold_limit", err)
	case errors.Is(err, api.ErrSalesClosed):
		return httpx.Wrap(http.StatusConflict, "sales_closed", err)
	case errors.Is(err, api.ErrSeatNotInEvent):
		return httpx.Wrap(http.StatusUnprocessableEntity, "seat_not_in_event", err)
	case errors.Is(err, api.ErrAdmissionRequired):
		return httpx.Wrap(http.StatusUnauthorized, "admission_required", err)
	case errors.Is(err, api.ErrHoldNotFound), errors.Is(err, api.ErrOrderNotFound):
		return httpx.Wrap(http.StatusNotFound, "not_found", err)
	case errors.Is(err, catalogapi.ErrEventNotFound), errors.Is(err, catalogapi.ErrSeatNotFound):
		return httpx.Wrap(http.StatusNotFound, "not_found", err)
	case errors.Is(err, api.ErrHoldNotActive), errors.Is(err, api.ErrHoldExpired), errors.Is(err, api.ErrHoldAlreadyOrdered):
		return httpx.Wrap(http.StatusConflict, "hold_state", err)
	case errors.Is(err, api.ErrOrderExpired):
		return httpx.Wrap(http.StatusConflict, "order_expired", err)
	case errors.Is(err, api.ErrOrderNotPending):
		return httpx.Wrap(http.StatusConflict, "order_state", err)
	case errors.Is(err, api.ErrPaymentFailed):
		return httpx.Wrap(http.StatusPaymentRequired, "payment_failed", err)
	case errors.Is(err, api.ErrIdempotencyConflict):
		return httpx.Wrap(http.StatusConflict, "idempotency_conflict", err)
	case errors.Is(err, api.ErrHoldsMismatch), errors.Is(err, api.ErrValidation):
		return httpx.Wrap(http.StatusUnprocessableEntity, "validation", err)
	}
	return err
}
