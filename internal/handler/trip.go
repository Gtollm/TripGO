package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	api "github.com/Gtollm/TripGO/internal/generated"
)

func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	body, err := decodeCreateTrip(w, r)
	if err != nil {
		h.logger.Info("invalid create trip request", "error", err, "path", r.URL.Path)
		h.writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", "invalid-request", "Request validation failed")
		return
	}

	created, repeated, err := h.trips.Create(
		r.Context(),
		params.IdempotencyKey,
		body.UserId,
		body.DriverId,
		trip.Coordinates{Latitude: body.StartPoint.Latitude, Longitude: body.StartPoint.Longitude},
		trip.Coordinates{Latitude: body.EndPoint.Latitude, Longitude: body.EndPoint.Longitude},
		body.Price,
	)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	response, err := toAPI(created)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	if repeated {
		if err := writeJSON(w, http.StatusOK, response); err != nil {
			h.logger.Error("write repeated trip response", "error", err)
		}
		return
	}

	h.logger.Info("trip created", "trip_id", created.ID.String())
	w.Header().Set("Location", "/api/v1/trips/"+created.ID.String())
	if err := writeJSON(w, http.StatusCreated, response); err != nil {
		h.logger.Error("write create trip response", "error", err)
	}
}

func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	found, err := h.trips.Get(r.Context(), tripID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	response, err := toAPI(found)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, response); err != nil {
		h.logger.Error("write get trip response", "error", err)
	}
}

func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	finished, err := h.trips.Finish(r.Context(), tripID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	response, err := toAPI(finished)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	h.logger.Info("trip finished", "trip_id", finished.ID.String())
	if err := writeJSON(w, http.StatusOK, response); err != nil {
		h.logger.Error("write finish trip response", "error", err)
	}
}

func toAPI(item trip.Trip) (api.Trip, error) {
	var status api.TripStatus
	switch item.Status {
	case trip.StatusACTIVE:
		status = api.Active
	case trip.StatusCOMPLETED:
		status = api.Completed
	default:
		return api.Trip{}, fmt.Errorf("unexpected trip status %q", item.Status)
	}

	return api.Trip{
		Id:         item.ID,
		UserId:     item.UserID,
		DriverId:   item.DriverID,
		StartPoint: api.Coordinates{Latitude: item.Start.Latitude, Longitude: item.Start.Longitude},
		EndPoint:   api.Coordinates{Latitude: item.End.Latitude, Longitude: item.End.Longitude},
		Price:      item.Price,
		Status:     status,
		StartedAt:  item.StartedAt,
		FinishedAt: item.FinishedAt,
	}, nil
}

func decodeCreateTrip(w http.ResponseWriter, r *http.Request) (api.TripData, error) {
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		return api.TripData{}, err
	}

	var top map[string]json.RawMessage
	if err := decodeOne(payload, &top); err != nil {
		return api.TripData{}, err
	}
	if err := allowedKeys(top, "user_id", "driver_id", "start_point", "end_point", "price"); err != nil {
		return api.TripData{}, err
	}
	if err := requirePoint(top["start_point"], "start_point"); err != nil {
		return api.TripData{}, err
	}
	if err := requirePoint(top["end_point"], "end_point"); err != nil {
		return api.TripData{}, err
	}

	var body api.TripData
	if err := decodeOne(payload, &body); err != nil {
		return api.TripData{}, err
	}
	return body, nil
}

func requirePoint(raw json.RawMessage, field string) error {
	var point map[string]json.RawMessage
	if err := json.Unmarshal(raw, &point); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	if err := allowedKeys(point, "latitude", "longitude"); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

func allowedKeys(fields map[string]json.RawMessage, required ...string) error {
	allowed := make(map[string]struct{}, len(required))
	for _, key := range required {
		allowed[key] = struct{}{}
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("%s is required", key)
		}
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("%s is not allowed", key)
		}
	}
	return nil
}

func decodeOne(payload []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single json value")
	}
	return nil
}
