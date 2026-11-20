package create_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	api "github.com/Gtollm/TripGO/internal/generated"
	transport "github.com/Gtollm/TripGO/internal/http"
	"github.com/Gtollm/TripGO/internal/http/position/create"
	"github.com/Gtollm/TripGO/internal/model"
)

const validBody = `{"coordinates":{"latitude":59.935,"longitude":30.335},"recorded_at":"2026-08-24T12:00:05Z"}`

func TestHandler(t *testing.T) {
	t.Parallel()

	recordedAt := time.Date(2026, 8, 24, 12, 0, 5, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		failure error
		status  int
		code    string
	}{
		{"created", nil, http.StatusCreated, ""},
		{"validation", &trip.ValidationError{Field: "recorded_at", Reason: "in future"}, http.StatusBadRequest, "invalid_request"},
		{"not found", trip.ErrTripNotFound, http.StatusNotFound, "trip_not_found"},
		{"completed", trip.ErrTripCompleted, http.StatusConflict, "trip_completed"},
		{"internal", errors.New("secret pgx error"), http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u := NewMockusecase(gomock.NewController(t))
			tripID := uuid.New()
			saved := model.Position{ID: 42, TripID: tripID, Coordinates: trip.Coordinates{Latitude: 59.935, Longitude: 30.335}, RecordedAt: recordedAt}
			u.EXPECT().Run(gomock.Any(), model.CreatePosition{
				TripID:      tripID,
				Coordinates: saved.Coordinates,
				RecordedAt:  recordedAt,
			}).Return(model.CreatePositionResult{Position: saved}, tc.failure)

			router := transport.NewRouter(nil, nil, nil, create.New(u), nil, nil)
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+tripID.String()+"/positions", strings.NewReader(validBody)))

			require.Equal(t, tc.status, resp.Code)
			if tc.failure != nil {
				var body api.Problem
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
				require.Equal(t, tc.code, body.Code)
				require.NotContains(t, resp.Body.String(), "secret pgx error")
				return
			}

			var body api.TripPosition
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			require.EqualValues(t, 42, body.Id)
			require.Equal(t, tripID, body.TripId)
		})
	}
}

func TestInvalidRequests(t *testing.T) {
	t.Parallel()

	tripPath := "/api/v1/trips/" + uuid.NewString() + "/positions"
	for _, tc := range []struct{ name, path, body string }{
		{"invalid uuid", "/api/v1/trips/not-a-uuid/positions", validBody},
		{"malformed json", tripPath, "{"},
		{"missing fields", tripPath, "{}"},
		{"unknown field", tripPath, strings.Replace(validBody, `"recorded_at"`, `"extra":1,"recorded_at"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u := NewMockusecase(gomock.NewController(t))
			resp := httptest.NewRecorder()
			transport.NewRouter(nil, nil, nil, create.New(u), nil, nil).
				ServeHTTP(resp, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body)))

			require.Equal(t, http.StatusBadRequest, resp.Code)
			require.Contains(t, resp.Body.String(), "invalid_request")
		})
	}
}
