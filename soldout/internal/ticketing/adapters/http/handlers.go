// Package http — GET /v1/users/{id}/tickets.
package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	bookingapi "github.com/nikolaysavelev/soldout/internal/booking/api"
	"github.com/nikolaysavelev/soldout/internal/platform/httpx"
	"github.com/nikolaysavelev/soldout/internal/ticketing/api"
)

// Handler — хендлеры билетов. Заказы пользователя читаются через booking.api (владелец orders).
type Handler struct {
	svc    api.Service
	orders bookingapi.Service
}

// New создаёт хендлеры.
func New(svc api.Service, orders bookingapi.Service) *Handler {
	return &Handler{svc: svc, orders: orders}
}

// Mount регистрирует маршруты.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/users/{id}/tickets", h.userTickets)
}

func (h *Handler) userTickets(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, "bad_request", "id должен быть UUID"))
		return
	}
	orders, err := h.orders.ListUserOrders(r.Context(), userID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ids := make([]uuid.UUID, 0, len(orders))
	for _, o := range orders {
		if o.Status == bookingapi.OrderPaid {
			ids = append(ids, o.ID)
		}
	}
	tickets, err := h.svc.ListByOrders(r.Context(), ids)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"tickets": tickets})
}
