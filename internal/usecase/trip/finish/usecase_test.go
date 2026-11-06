package finish_test

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
	"github.com/Gtollm/TripGO/internal/usecase/trip/finish"
)

const (
	successScenario   = "success"
	notFoundScenario  = "not found"
	completedScenario = "completed"
)

func TestFinish(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{successScenario, notFoundScenario, completedScenario, "read error", "write error", "begin error", "commit error"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			repo, tx, metrics := NewMocktripRepository(ctrl), NewMocktxManager(ctrl), NewMockmetrics(ctrl)
			ctx := t.Context()
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			item := model.Trip{
				ID:        uuid.New(),
				Status:    trip.StatusACTIVE,
				StartedAt: now.Add(-time.Hour),
			}
			if scenario == completedScenario {
				item.Status = trip.StatusCOMPLETED
				item.FinishedAt = &now
			}

			failure := errors.New("database failed")
			committed := false
			if scenario == "begin error" {
				tx.EXPECT().Do(ctx, gomock.Any()).Return(failure)
			} else {
				tx.EXPECT().Do(ctx, gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
					if err := fn(ctx); err != nil {
						return err
					}

					if scenario == "commit error" {
						return failure
					}

					committed = true
					return nil
				})
				var fetchErr error
				if scenario == notFoundScenario {
					fetchErr = trip.ErrTripNotFound
				}

				if scenario == "read error" {
					fetchErr = failure
				}

				repo.EXPECT().GetForUpdate(ctx, item.ID).Return(item, fetchErr)
				if fetchErr == nil && scenario != completedScenario {
					repo.EXPECT().UpdateStatus(ctx, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, saved model.Trip, change model.Transition) error {
						require.Equal(t, item.ID, saved.ID)
						require.Equal(t, trip.StatusCOMPLETED, saved.Status)
						require.Equal(t, &now, saved.FinishedAt)
						require.Equal(t, model.Transition{
							From:   trip.StatusACTIVE,
							To:     trip.StatusCOMPLETED,
							At:     now,
							Reason: "finished",
						}, change)
						if scenario == "write error" {
							return failure
						}

						return nil
					})
				}
			}

			if scenario == successScenario {
				metrics.EXPECT().Completed(ctx).Do(func(context.Context) { require.True(t, committed) })
			}

			u := finish.New(repo, tx, metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now })
			got, err := u.Run(ctx, model.FinishTrip{ID: item.ID})
			switch scenario {
			case successScenario:
				require.NoError(t, err)
				require.Equal(t, &now, got.Trip.FinishedAt)
				require.Equal(t, item.ID, got.Trip.ID)
			case notFoundScenario:
				require.ErrorIs(t, err, trip.ErrTripNotFound)
			case completedScenario:
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
