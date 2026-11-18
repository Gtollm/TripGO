package domainerror

import (
	"errors"
	"net/http"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	api "github.com/Gtollm/TripGO/internal/generated"
)

var (
	ErrInvalidRequest   = errors.New("request validation failed")
	ErrRouteNotFound    = errors.New("route not found")
	ErrMethodNotAllowed = errors.New("method not allowed")
)

func ToProblem(err error) (int, api.Problem) {
	var invalid *trip.ValidationError
	switch {
	case errors.As(err, &invalid):
		return toProblem(http.StatusBadRequest, "invalid_request", "Invalid request", "invalid-request", invalid.Error())
	case errors.Is(err, ErrInvalidRequest):
		return toProblem(http.StatusBadRequest, "invalid_request", "Invalid request", "invalid-request", "Request validation failed")
	case errors.Is(err, trip.ErrTripNotFound):
		return toProblem(http.StatusNotFound, "trip_not_found", "Trip not found", "trip-not-found", "Trip was not found")
	case errors.Is(err, trip.ErrTripCompleted):
		return toProblem(http.StatusConflict, "trip_completed", "Trip completed", "trip-completed", "Operation is not allowed for a completed trip")
	case errors.Is(err, trip.ErrDriverBusy):
		return toProblem(http.StatusConflict, "driver_busy", "Driver busy", "driver-busy", "Driver already has an active trip")
	case errors.Is(err, trip.ErrIdempotencyConflict):
		return toProblem(http.StatusConflict, "idempotency_conflict", "Idempotency conflict", "idempotency-conflict", "Idempotency-Key was already used with a different request body")
	case errors.Is(err, ErrRouteNotFound):
		return toProblem(http.StatusNotFound, "not_found", "Not found", "not-found", "Route was not found")
	case errors.Is(err, ErrMethodNotAllowed):
		return toProblem(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "method-not-allowed", "Method is not allowed for this route")
	default:
		return toProblem(http.StatusInternalServerError, "internal_error", "Internal Server Error", "internal-error", "Internal server error")
	}
}

func toProblem(status int32, code, title, kind, detail string) (int, api.Problem) {
	return int(status), api.Problem{
		Status: status, Code: code, Title: title,
		Type: "https://tripgo.example/problems/" + kind, Detail: &detail,
	}
}
