package create_test

import (
	"context"
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
	"github.com/Gtollm/TripGO/internal/http/trip/create"
	"github.com/Gtollm/TripGO/internal/model"
)

const validBody = `{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","start_point":{"latitude":0,"longitude":0},"end_point":{"latitude":90,"longitude":180},"price":0}`

func TestCreate(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		repeated bool
		failure  error
		status   int
		code     string
	}{
		{"created", false, nil, 201, ""},
		{"repeated", true, nil, 200, ""},
		{"busy", false, trip.ErrDriverBusy, 409, "driver_busy"},
		{"conflict", false, trip.ErrIdempotencyConflict, 409, "idempotency_conflict"},
		{"validation", false, &trip.ValidationError{Field: "price", Reason: "invalid"}, 400, "invalid_request"},
		{"internal", false, errors.New("secret SQL"), 500, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u := NewMockusecase(gomock.NewController(t))
			item := model.Trip{ID: uuid.New(), Status: trip.StatusACTIVE, StartedAt: time.Now()}
			key := uuid.New()
			u.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, input model.CreateTrip) (model.CreatedTrip, error) {
				require.Equal(t, &key, input.IdempotencyKey)
				require.Equal(t, uuid.MustParse("5cb72c04-7650-45c9-a79b-bcdba0631e0c"), input.UserID)
				require.EqualValues(t, 0, input.Price)
				require.Equal(t, trip.Coordinates{Latitude: 90, Longitude: 180}, input.End)
				return model.CreatedTrip{Trip: item, Repeated: tc.repeated}, tc.failure
			})
			router := transport.NewRouter(create.New(u), nil, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(validBody))
			req.Header.Set("Idempotency-Key", key.String())
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			require.Equal(t, tc.status, response.Code)
			if tc.failure != nil {
				var body api.Problem
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				require.Equal(t, tc.code, body.Code)
				require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
				require.NotContains(t, response.Body.String(), "secret SQL")
				return
			}

			require.Equal(t, "application/json", response.Header().Get("Content-Type"))

			var body api.Trip
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, item.ID, body.Id)
			if tc.repeated {
				require.Empty(t, response.Header().Get("Location"))
			} else {
				require.Equal(t, "/api/v1/trips/"+item.ID.String(), response.Header().Get("Location"))
			}
		})
	}
}

func TestInvalidBody(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
	}{
		{"empty body", ""},
		{"invalid JSON", "{"},
		{"missing fields", "{}"},
		{"multiple JSON values", validBody + " {}"},
		{"null price", strings.Replace(validBody, `"price":0`, `"price":null`, 1)},
		{"unknown field", strings.Replace(validBody, `"price":0`, `"price":0,"extra":1`, 1)},
		{"missing latitude", strings.Replace(validBody, `"latitude":0,"longitude":0`, `"longitude":0`, 1)},
		{"unknown coordinate", strings.Replace(validBody, `"latitude":0`, `"latitude":0,"extra":1`, 1)},
		{"fractional price", strings.Replace(validBody, `"price":0`, `"price":0.5`, 1)},
		{"body too large", strings.Repeat(" ", (1<<20)+1) + validBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u := NewMockusecase(gomock.NewController(t))
			router := transport.NewRouter(create.New(u), nil, nil, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(tc.body)))
			require.Equal(t, 400, response.Code)

			var problem api.Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			require.Equal(t, "invalid_request", problem.Code)
		})
	}
}

func TestInvalidKey(t *testing.T) {
	t.Parallel()

	for _, keys := range [][]string{{"not-a-uuid"}, {uuid.NewString(), uuid.NewString()}} {
		t.Run("invalid header", func(t *testing.T) {
			t.Parallel()

			u := NewMockusecase(gomock.NewController(t))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(validBody))
			req.Header["Idempotency-Key"] = keys
			response := httptest.NewRecorder()
			transport.NewRouter(create.New(u), nil, nil, nil).ServeHTTP(response, req)
			require.Equal(t, 400, response.Code)
			require.Contains(t, response.Body.String(), "invalid_request")
		})
	}
}
