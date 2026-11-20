package create_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/model"
	"github.com/Gtollm/TripGO/internal/usecase/position/create"
)

const (
	scenarioSuccess   = "success"
	scenarioNotFound  = "not found"
	scenarioCompleted = "completed"
	scenarioInvalid   = "invalid"
)

func TestCreatePosition(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{scenarioSuccess, scenarioInvalid, scenarioNotFound, scenarioCompleted, "insert error", "update error"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			trips, positions := NewMocktripRepository(ctrl), NewMockpositionRepository(ctrl)
			tx, canceller, metrics := NewMocktxManager(ctrl), NewMockqueueCanceller(ctrl), NewMockmetrics(ctrl)

			ctx := t.Context()
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			tripID := uuid.New()
			coords := trip.Coordinates{Latitude: 59.935, Longitude: 30.335}
			if scenario == scenarioInvalid {
				coords.Latitude = 91
			}

			stored := model.Trip{ID: tripID, Status: trip.StatusACTIVE, StartedAt: now.Add(-time.Hour)}
			if scenario == scenarioCompleted {
				stored.Status = trip.StatusCOMPLETED
			}

			recordedAt := now.Add(-5 * time.Second)
			saved := model.Position{ID: 42, TripID: tripID, Coordinates: coords, RecordedAt: recordedAt}
			failure := errors.New("database failed")
			committed := false

			if scenario != scenarioInvalid {
				tx.EXPECT().Do(ctx, gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
					if err := fn(ctx); err != nil {
						return err
					}
					committed = true
					return nil
				})

				var getErr error
				if scenario == scenarioNotFound {
					getErr = trip.ErrTripNotFound
				}
				trips.EXPECT().GetForUpdate(ctx, tripID).Return(stored, getErr)

				if getErr == nil && scenario != scenarioCompleted {
					var insertErr error
					if scenario == "insert error" {
						insertErr = failure
					}
					positions.EXPECT().Create(ctx, model.Position{TripID: tripID, Coordinates: coords, RecordedAt: recordedAt}, now).Return(saved, insertErr)

					if insertErr == nil {
						var updateErr error
						if scenario == "update error" {
							updateErr = failure
						}
						trips.EXPECT().UpdateLastPositionAt(ctx, tripID, recordedAt, now).Return(updateErr)
					}
				}
			}

			if scenario == scenarioSuccess {
				canceller.EXPECT().Cancel(ctx, tripID).Do(func(context.Context, uuid.UUID) { require.True(t, committed) })
				metrics.EXPECT().PositionSaved(ctx).Do(func(context.Context) { require.True(t, committed) })
			}

			uc := create.New(trips, positions, tx, canceller, metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now })
			got, err := uc.Run(ctx, model.CreatePosition{TripID: tripID, Coordinates: coords, RecordedAt: recordedAt})

			switch scenario {
			case scenarioSuccess:
				require.NoError(t, err)
				require.Equal(t, saved, got.Position)
			case scenarioInvalid:
				var invalid *trip.ValidationError
				require.ErrorAs(t, err, &invalid)
			case scenarioNotFound:
				require.ErrorIs(t, err, trip.ErrTripNotFound)
			case scenarioCompleted:
				require.ErrorIs(t, err, trip.ErrTripCompleted)
			default:
				require.ErrorIs(t, err, failure)
			}
			if err != nil {
				require.Empty(t, got)
			}
		})
	}
}
