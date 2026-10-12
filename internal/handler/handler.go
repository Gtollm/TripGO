package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	api "github.com/Gtollm/TripGO/internal/generated"
	"github.com/Gtollm/TripGO/internal/service"
)

const maxRequestBody = 1 << 20

type readiness interface {
	Ping(context.Context) error
}

type Handler struct {
	trips        *service.Service
	ready        readiness
	logger       *slog.Logger
	queryTimeout time.Duration
}

func New(trips *service.Service, ready readiness, logger *slog.Logger, queryTimeout time.Duration) *Handler {
	return &Handler{
		trips:        trips,
		ready:        ready,
		logger:       logger,
		queryTimeout: queryTimeout,
	}
}

func (h *Handler) Routes() http.Handler {
	router := chi.NewRouter()
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		h.writeProblem(w, r, http.StatusNotFound, "not_found", "Not found", "not-found", "Route was not found")
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		h.writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "method-not-allowed", "Method is not allowed for this route")
	})
	return api.HandlerWithOptions(
		h, api.ChiServerOptions{
			BaseRouter:       router,
			ErrorHandlerFunc: h.handleParamError,
		},
	)
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	if err := writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok}); err != nil {
		h.logger.Error("write health response", "error", err)
	}
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.queryTimeout)
	defer cancel()

	status := http.StatusOK
	body := api.HealthResponse{Status: api.Ok}
	if err := h.ready.Ping(ctx); err != nil {
		h.logger.Warn("postgres is not ready", "error", err)
		status = http.StatusServiceUnavailable
		body.Status = api.Unavailable
	}

	if err := writeJSON(w, status, body); err != nil {
		h.logger.Error("write ready response", "error", err)
	}
}

func (h *Handler) handleParamError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Info("invalid request parameter", "error", err, "path", r.URL.Path)
	h.writeProblem(
		w, r, http.StatusBadRequest, "invalid_request", "Invalid request", "invalid-request",
		"Request validation failed",
	)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *trip.TripGoError
	switch {
	case errors.As(err, &invalid):
		h.logger.Info("invalid trip", "error", err, "path", r.URL.Path)
		h.writeProblem(
			w, r, http.StatusBadRequest, "invalid_request", "Invalid request", "invalid-request", invalid.Error(),
		)
	case errors.Is(err, trip.ErrTripNotFound):
		h.writeProblem(
			w, r, http.StatusNotFound, "trip_not_found", "Trip not found", "trip-not-found", "Trip was not found",
		)
	case errors.Is(err, trip.ErrTripCompleted):
		h.writeProblem(
			w, r, http.StatusConflict, "trip_completed", "Trip completed", "trip-completed",
			"Operation is not allowed for a completed trip",
		)
	case errors.Is(err, trip.ErrDriverBusy):
		h.writeProblem(
			w, r, http.StatusConflict, "driver_busy", "Driver busy", "driver-busy", "Driver already has an active trip",
		)
	case errors.Is(err, trip.ErrIdempotencyConflict):
		h.writeProblem(
			w, r, http.StatusConflict, "idempotency_conflict", "Idempotency conflict", "idempotency-conflict",
			"Idempotency-Key was already used with a different request body",
		)
	default:
		h.logger.Error("request failed", "error", err, "path", r.URL.Path)
		h.writeProblem(
			w, r, http.StatusInternalServerError, "internal_error", "Internal Server Error", "internal-error",
			"Internal server error",
		)
	}
}

func (h *Handler) writeProblem(w http.ResponseWriter, r *http.Request, status int32, code, title, kind, detail string) {
	instance := r.URL.Path
	body := api.Problem{
		Type:     "https://tripgo.example/problems/" + kind,
		Title:    title,
		Status:   status,
		Code:     code,
		Detail:   &detail,
		Instance: &instance,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(int(status))
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.Error("write problem response", "error", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		return fmt.Errorf("encode json response: %w", err)
	}
	return nil
}
