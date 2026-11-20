package list_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/model"
	"github.com/Gtollm/TripGO/internal/usecase/position/list"
)

func TestListPositions(t *testing.T) {
	t.Parallel()

	tripID := uuid.New()
	recorded := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	items := []model.Position{{ID: 1, TripID: tripID, Coordinates: trip.Coordinates{Latitude: 59.93, Longitude: 30.33}, RecordedAt: recorded}}
	repoErr := errors.New("query failed")

	for _, tc := range []struct {
		name    string
		getErr  error
		listErr error
	}{
		{"success", nil, nil},
		{"not found", trip.ErrTripNotFound, nil},
		{"list error", nil, repoErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			trips, positions := NewMocktripRepository(ctrl), NewMockpositionRepository(ctrl)

			trips.EXPECT().Get(t.Context(), tripID).Return(model.Trip{ID: tripID}, tc.getErr)
			if tc.getErr == nil {
				positions.EXPECT().ListByTrip(t.Context(), tripID).Return(items, tc.listErr)
			}

			got, err := list.New(trips, positions).Run(t.Context(), model.ListPositions{TripID: tripID})
			wantErr := tc.getErr
			if wantErr == nil {
				wantErr = tc.listErr
			}
			if wantErr != nil {
				require.ErrorIs(t, err, wantErr)
				require.Empty(t, got)
				return
			}

			require.NoError(t, err)
			require.Equal(t, items, got.Positions)
		})
	}
}
