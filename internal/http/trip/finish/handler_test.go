package finish_test

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
	"github.com/Gtollm/TripGO/internal/http/trip/finish"
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
		{trip.ErrTripCompleted, 409, "trip_completed"},
	} {
		t.Run("response", func(t *testing.T) {
			t.Parallel()

			u := NewMockusecase(gomock.NewController(t))
			id := uuid.New()
			item := model.Trip{ID: id, Status: trip.StatusACTIVE}
			u.EXPECT().Run(gomock.Any(), model.FinishTrip{ID: id}).Return(model.FinishTripResult{Trip: item}, tc.failure)
			router := transport.NewRouter(nil, nil, finish.New(u), nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", nil))
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
