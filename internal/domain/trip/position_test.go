package trip_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Gtollm/TripGO/internal/domain/trip"
)

func TestNewPosition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tripID := uuid.New()
	coords := trip.Coordinates{Latitude: 59.935, Longitude: 30.335}
	recordedAt := now.Add(time.Minute)

	got, err := trip.NewPosition(tripID, coords, recordedAt, now)
	require.NoError(t, err)
	require.Equal(t, trip.Position{
		TripID:      tripID,
		Coordinates: coords,
		RecordedAt:  recordedAt,
	}, got)
}

func TestNewPositionValidation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	type input struct {
		tripID     uuid.UUID
		coords     trip.Coordinates
		recordedAt time.Time
	}

	for _, tc := range []struct {
		name, field string
		mutate      func(*input)
	}{
		{"empty trip id", "trip_id", func(in *input) { in.tripID = uuid.Nil }},
		{"latitude out of range", "coordinates.latitude", func(in *input) { in.coords.Latitude = 90.001 }},
		{"longitude out of range", "coordinates.longitude", func(in *input) { in.coords.Longitude = -180.001 }},
		{"zero recorded_at", "recorded_at", func(in *input) { in.recordedAt = time.Time{} }},
		{"recorded_at in future", "recorded_at", func(in *input) { in.recordedAt = now.Add(time.Minute + time.Nanosecond) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := input{
				tripID:     uuid.New(),
				coords:     trip.Coordinates{Latitude: 59.935, Longitude: 30.335},
				recordedAt: now,
			}
			tc.mutate(&in)

			got, err := trip.NewPosition(in.tripID, in.coords, in.recordedAt, now)
			var invalid *trip.ValidationError
			require.ErrorAs(t, err, &invalid)
			require.Equal(t, tc.field, invalid.Field)
			require.Empty(t, got)
		})
	}
}

func TestEnsureActive(t *testing.T) {
	t.Parallel()

	item := trip.Trip{Status: trip.StatusACTIVE}
	require.NoError(t, item.EnsureActive())

	completed := trip.Trip{Status: trip.StatusCOMPLETED}
	require.ErrorIs(t, completed.EnsureActive(), trip.ErrTripCompleted)

	unknown := trip.Trip{Status: trip.StatusNONE}
	var invalid *trip.ValidationError
	require.ErrorAs(t, unknown.EnsureActive(), &invalid)
}
