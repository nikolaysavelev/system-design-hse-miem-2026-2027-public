// Package http — POST /v1/events/{id}/queue/join.
package http

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	catalogapi "github.com/nikolaysavelev/soldout/internal/catalog/api"
	"github.com/nikolaysavelev/soldout/internal/platform/httpx"
	"github.com/nikolaysavelev/soldout/internal/queue/api"
)

// Handler — хендлеры waiting room.
type Handler struct{ svc api.Service }

// New создаёт хендлеры.
func New(svc api.Service) *Handler { return &Handler{svc: svc} }

// Mount регистрирует маршруты.
func (h *Handler) Mount(r chi.Router) {
	r.Post("/v1/events/{id}/queue/join", h.join)
	r.Get("/v1/events/{id}/queue/status", h.status)
	r.Get("/v1/events/{id}/queue/stats", h.stats)
}

type joinRequest struct {
	UserID uuid.UUID `json:"user_id"`
}

func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	eventID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "bad_request", "id должен быть UUID"))
		return
	}
	var in joinRequest
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.svc.Join(r.Context(), eventID, in.UserID)
	if err != nil {
		switch {
		case errors.Is(err, catalogapi.ErrEventNotFound):
			httpx.WriteError(w, r, httpx.Wrap(http.StatusNotFound, "event_not_found", err))
		case errors.Is(err, api.ErrAdmissionInvalid):
			httpx.WriteError(w, r, httpx.Wrap(http.StatusUnprocessableEntity, "validation", err))
		default:
			httpx.WriteError(w, r, err)
		}
		return
	}
	httpx.JSON(w, http.StatusOK, a)
}

// status — GET /v1/events/{id}/queue/status?user_id=…; пока не допущен — Retry-After: 1.
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	eventID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "bad_request", "id должен быть UUID"))
		return
	}
	userID, err := uuid.Parse(r.URL.Query().Get("user_id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "bad_request", "user_id должен быть UUID"))
		return
	}
	st, err := h.svc.Status(r.Context(), eventID, userID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !st.Admitted {
		w.Header().Set("Retry-After", "1")
	}
	httpx.JSON(w, http.StatusOK, st)
}

// stats — GET /v1/events/{id}/queue/stats.
func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	eventID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "bad_request", "id должен быть UUID"))
		return
	}
	st, err := h.svc.Stats(r.Context(), eventID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}
