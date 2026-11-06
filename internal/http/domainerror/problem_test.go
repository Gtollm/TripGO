package domainerror_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/http/domainerror"
)

func TestToProblem(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{&trip.ValidationError{Field: "price", Reason: "must be nonnegative"}, 400, "invalid_request"},
		{domainerror.ErrInvalidRequest, 400, "invalid_request"},
		{trip.ErrTripNotFound, 404, "trip_not_found"},
		{trip.ErrTripCompleted, 409, "trip_completed"},
		{trip.ErrDriverBusy, 409, "driver_busy"},
		{trip.ErrIdempotencyConflict, 409, "idempotency_conflict"},
		{domainerror.ErrRouteNotFound, 404, "not_found"},
		{domainerror.ErrMethodNotAllowed, 405, "method_not_allowed"},
		{errors.New("private SQL postgres password"), 500, "internal_error"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()

			for _, err := range []error{tc.err, fmt.Errorf("outer: %w", tc.err)} {
				status, body := domainerror.ToProblem(err)
				require.Equal(t, tc.status, status)
				require.EqualValues(t, status, body.Status)
				require.Equal(t, tc.code, body.Code)
				require.NotEmpty(t, body.Title)
				require.Contains(t, body.Type, "https://tripgo.example/problems/")
				require.NotNil(t, body.Detail)
				if status == 500 {
					require.Equal(t, "Internal server error", *body.Detail)
					require.NotContains(t, *body.Detail, "private")
				}
			}
		})
	}
}
