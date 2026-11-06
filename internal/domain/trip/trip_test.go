package trip_test

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Gtollm/TripGO/internal/domain/trip"
)

func TestNew(t *testing.T) {
	t.Parallel()

	id, user, driver := uuid.New(), uuid.New(), uuid.New()
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	start, end := trip.Coordinates{Latitude: -90, Longitude: 180}, trip.Coordinates{Latitude: 90, Longitude: -180}
	got, change, err := trip.New(id, user, driver, start, end, 0, at)
	require.NoError(t, err)
	require.Equal(
		t, trip.Trip{
			ID:        id,
			UserID:    user,
			DriverID:  driver,
			Start:     start,
			End:       end,
			Price:     0,
			Status:    trip.StatusACTIVE,
			StartedAt: at,
		}, got,
	)
	require.Equal(
		t, trip.Transition{
			From:   trip.StatusNONE,
			To:     trip.StatusACTIVE,
			At:     at,
			Reason: "created",
		}, change,
	)
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	type input struct {
		id, user, driver uuid.UUID
		start, end       trip.Coordinates
		price            int64
		at               time.Time
	}

	cases := []struct {
		name, field string
		change      func(*input)
	}{
		{"empty id", "id", func(v *input) { v.id = uuid.Nil }},
		{"empty user", "user_id", func(v *input) { v.user = uuid.Nil }},
		{"empty driver", "driver_id", func(v *input) { v.driver = uuid.Nil }},
		{"negative price", "price", func(v *input) { v.price = -1 }},
		{"zero time", "started_at", func(v *input) { v.at = time.Time{} }},
		{"start latitude low", "start_point.latitude", func(v *input) { v.start.Latitude = -90.001 }},
		{"start latitude high", "start_point.latitude", func(v *input) { v.start.Latitude = 90.001 }},
		{"start longitude low", "start_point.longitude", func(v *input) { v.start.Longitude = -180.001 }},
		{"start longitude high", "start_point.longitude", func(v *input) { v.start.Longitude = 180.001 }},
		{"end latitude", "end_point.latitude", func(v *input) { v.end.Latitude = math.NaN() }},
		{"end longitude", "end_point.longitude", func(v *input) { v.end.Longitude = math.NaN() }},
		{"infinite latitude", "end_point.latitude", func(v *input) { v.end.Latitude = math.Inf(1) }},
		{"infinite longitude", "end_point.longitude", func(v *input) { v.end.Longitude = math.Inf(-1) }},
	}

	for _, tc := range cases {
		t.Run(
			tc.name, func(t *testing.T) {
				t.Parallel()

				v := input{
					id:     uuid.New(),
					user:   uuid.New(),
					driver: uuid.New(),
					price:  1,
					at:     time.Now(),
				}
				tc.change(&v)
				got, change, err := trip.New(v.id, v.user, v.driver, v.start, v.end, v.price, v.at)
				var invalid *trip.ValidationError
				require.ErrorAs(t, err, &invalid)
				require.Equal(t, tc.field, invalid.Field)
				require.NotEmpty(t, invalid.Reason)
				require.Contains(t, err.Error(), tc.field)
				require.Empty(t, got)
				require.Empty(t, change)
			},
		)
	}
}

func TestFinish(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		status    trip.Status
		at        time.Time
		invalid   bool
		completed bool
	}{
		{"active", trip.StatusACTIVE, started.Add(time.Minute), false, false},
		{"same instant", trip.StatusACTIVE, started, false, false},
		{"already completed", trip.StatusCOMPLETED, started.Add(time.Hour), false, true},
		{"unknown status", trip.StatusNONE, started, true, false},
		{"empty time", trip.StatusACTIVE, time.Time{}, true, false},
		{"before start", trip.StatusACTIVE, started.Add(-time.Nanosecond), true, false},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				t.Parallel()

				item := trip.Trip{ID: uuid.New(), Status: tc.status, StartedAt: started}
				before := item
				change, err := item.Finish(tc.at)
				if tc.completed || tc.invalid {
					if tc.completed {
						require.ErrorIs(t, err, trip.ErrTripCompleted)
					} else {
						var invalid *trip.ValidationError
						require.ErrorAs(t, err, &invalid)
					}

					require.Equal(t, before, item)
					require.Empty(t, change)
					return
				}

				require.NoError(t, err)
				require.Equal(t, trip.StatusCOMPLETED, item.Status)
				require.Equal(t, &tc.at, item.FinishedAt)
				require.Equal(
					t, trip.Transition{
						From:   trip.StatusACTIVE,
						To:     trip.StatusCOMPLETED,
						At:     tc.at,
						Reason: "finished",
					}, change,
				)
				require.Equal(t, before.ID, item.ID)
				require.Equal(t, started, item.StartedAt)
			},
		)
	}
}
