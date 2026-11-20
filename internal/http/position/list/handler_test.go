package list_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	api "github.com/Gtollm/TripGO/internal/generated"
	transport "github.com/Gtollm/TripGO/internal/http"
	"github.com/Gtollm/TripGO/internal/http/position/list"
	"github.com/Gtollm/TripGO/internal/model"
)

func TestHandler(t *testing.T) {
	t.Parallel()

	tripID := uuid.New()
	pos := model.Position{ID: 1, TripID: tripID, Coordinates: trip.Coordinates{Latitude: 59.93, Longitude: 30.33}, RecordedAt: time.Date(2026, 8, 24, 12, 0, 5, 0, time.UTC)}

	for _, tc := range []struct {
		name      string
		positions []model.Position
		failure   error
		status    int
		code      string
	}{
		{"ok", []model.Position{pos}, nil, http.StatusOK, ""},
		{"empty", nil, nil, http.StatusOK, ""},
		{"not found", nil, trip.ErrTripNotFound, http.StatusNotFound, "trip_not_found"},
		{"internal", nil, errors.New("secret db failure"), http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u := NewMockusecase(gomock.NewController(t))
			u.EXPECT().Run(gomock.Any(), model.ListPositions{TripID: tripID}).Return(model.ListPositionsResult{Positions: tc.positions}, tc.failure)

			resp := httptest.NewRecorder()
			transport.NewRouter(nil, nil, nil, nil, list.New(u), nil).
				ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID.String()+"/positions", nil))

			require.Equal(t, tc.status, resp.Code)
			if tc.failure != nil {
				var problem api.Problem
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &problem))
				require.Equal(t, tc.code, problem.Code)
				require.NotContains(t, resp.Body.String(), "secret db failure")
				return
			}

			var body api.TripPositionsResponse
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			require.Len(t, body.Positions, len(tc.positions))
		})
	}
}

func TestInvalidID(t *testing.T) {
	t.Parallel()

	u := NewMockusecase(gomock.NewController(t))
	resp := httptest.NewRecorder()
	transport.NewRouter(nil, nil, nil, nil, list.New(u), nil).
		ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/trips/not-a-uuid/positions", nil))

	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "invalid_request")
}
