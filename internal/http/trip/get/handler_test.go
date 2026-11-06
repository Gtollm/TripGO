package get_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	api "github.com/Gtollm/TripGO/internal/generated"
	transport "github.com/Gtollm/TripGO/internal/http"
	"github.com/Gtollm/TripGO/internal/http/trip/get"
	"github.com/Gtollm/TripGO/internal/model"
)

func TestHandler(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		failure error
		status  int
		code    string
	}{
		{nil, 200, ""},
		{trip.ErrTripNotFound, 404, "trip_not_found"},
	} {
		t.Run("response", func(t *testing.T) {
			t.Parallel()

			u := NewMockusecase(gomock.NewController(t))
			id := uuid.New()
			item := model.Trip{ID: id, Status: trip.StatusACTIVE}
			u.EXPECT().Run(gomock.Any(), model.GetTrip{ID: id}).Return(model.GetTripResult{Trip: item}, tc.failure)
			router := transport.NewRouter(nil, get.New(u), nil, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), nil))
			require.Equal(t, tc.status, response.Code)
			if tc.failure != nil {
				var body api.Problem
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				require.Equal(t, tc.code, body.Code)
			} else {
				var body api.Trip
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				require.Equal(t, id, body.Id)
			}
		})
	}
}

func TestInvalidID(t *testing.T) {
	t.Parallel()

	u := NewMockusecase(gomock.NewController(t))
	router := transport.NewRouter(nil, get.New(u), nil, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/trips/not-a-uuid", nil))
	require.Equal(t, 400, response.Code)
	require.Contains(t, response.Body.String(), "invalid_request")
}
