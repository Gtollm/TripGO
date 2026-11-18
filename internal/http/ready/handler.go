package ready

import (
	"context"
	"net/http"
	"time"

	api "github.com/Gtollm/TripGO/internal/generated"
	"github.com/Gtollm/TripGO/internal/http/requeststate"
	"github.com/Gtollm/TripGO/internal/http/response"
)

type Handler struct {
	ready   readiness
	timeout time.Duration
}

func New(ready readiness, timeout time.Duration) *Handler {
	return &Handler{ready: ready, timeout: timeout}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	status := http.StatusOK
	body := api.HealthResponse{Status: api.Ok}
	if err := h.ready.Ping(ctx); err != nil {
		requeststate.Fail(r.Context(), err, "database_unavailable")
		status = http.StatusServiceUnavailable
		body.Status = api.Unavailable
	}

	response.ToJSON(w, r, status, body)
}
