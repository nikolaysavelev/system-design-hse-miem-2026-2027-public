// Package http — HTTP-хендлеры каталога (/v1/events, /v1/admin/events).
package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/catalog/api"
	"github.com/nikolaysavelev/soldout/internal/platform/httpx"
)

// Handler — хендлеры каталога.
type Handler struct{ svc api.Service }

// New создаёт хендлеры.
func New(svc api.Service) *Handler { return &Handler{svc: svc} }

// Mount регистрирует маршруты.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/events", h.list)
	r.Get("/v1/events/{id}", h.get)
	r.Get("/v1/events/{id}/seatmap", h.seatMap)
	r.Get("/v1/events/{id}/sectors/{sector_id}/seatmap", h.sectorSeatMap)
	r.Post("/v1/admin/events", h.create)
	r.Post("/v1/admin/events/{id}/open", h.open)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	events, err := h.svc.ListEvents(r.Context())
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	if events == nil {
		events = []api.Event{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "bad_request", "id должен быть UUID"))
		return
	}
	ev, err := h.svc.GetEvent(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, ev)
}

func (h *Handler) seatMap(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "bad_request", "id должен быть UUID"))
		return
	}
	sm, err := h.svc.SeatMap(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, sm)
}

// sectorSeatMap отдаёт предсобранный JSON сектора с ETag = версия из БД; If-None-Match → 304.
func (h *Handler) sectorSeatMap(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "bad_request", "id должен быть UUID"))
		return
	}
	sectorID, err := uuid.Parse(chi.URLParam(r, "sector_id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "bad_request", "sector_id должен быть UUID"))
		return
	}
	res, err := h.svc.SectorSeatMap(r.Context(), id, sectorID)
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	etag := `"` + strconv.FormatInt(res.Version, 10) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	switch {
	case res.Stale:
		w.Header().Set("X-Cache", "stale")
	case res.Cached:
		w.Header().Set("X-Cache", "hit")
	default:
		w.Header().Set("X-Cache", "miss")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(res.JSON)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in api.CreateEventInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ev, err := h.svc.CreateEvent(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	httpx.JSON(w, http.StatusCreated, ev)
}

func (h *Handler) open(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "bad_request", "id должен быть UUID"))
		return
	}
	ev, err := h.svc.OpenSales(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, mapErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, ev)
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, api.ErrEventNotFound):
		return httpx.Wrap(http.StatusNotFound, "event_not_found", err)
	case errors.Is(err, api.ErrSeatNotFound):
		return httpx.Wrap(http.StatusNotFound, "seat_not_found", err)
	case errors.Is(err, api.ErrSectorNotFound):
		return httpx.Wrap(http.StatusNotFound, "sector_not_found", err)
	case errors.Is(err, api.ErrVenueNotFound):
		return httpx.Wrap(http.StatusUnprocessableEntity, "venue_not_found", err)
	case errors.Is(err, api.ErrValidation):
		return httpx.Wrap(http.StatusUnprocessableEntity, "validation", err)
	case errors.Is(err, api.ErrInvalidTransition):
		return httpx.Wrap(http.StatusConflict, "invalid_transition", err)
	}
	return err
}
