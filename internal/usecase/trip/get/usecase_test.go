package get_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/model"
	"github.com/Gtollm/TripGO/internal/usecase/trip/get"
)

func TestGet(t *testing.T) {
	t.Parallel()

	for _, failure := range []error{nil, trip.ErrTripNotFound, errors.New("database failed")} {
		t.Run("result", func(t *testing.T) {
			t.Parallel()

			repo := NewMocktripRepository(gomock.NewController(t))
			want := model.Trip{ID: uuid.New(), Status: trip.StatusACTIVE}
			repo.EXPECT().Get(t.Context(), want.ID).Return(want, failure)
			got, err := get.New(repo).Run(t.Context(), model.GetTrip{ID: want.ID})
			if failure != nil {
				require.ErrorIs(t, err, failure)
				require.Empty(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, want, got.Trip)
			}
		})
	}
}
