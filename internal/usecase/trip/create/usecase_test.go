package create_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/model"
	"github.com/Gtollm/TripGO/internal/usecase/trip/create"
)

const (
	tripErrorScenario = "trip error"
	keyErrorScenario  = "key error"
	conflictScenario  = "conflict"
)

type fixture struct {
	u       *create.UseCase
	repo    *MocktripRepository
	keys    *MockidempotencyRepository
	tx      *MocktxManager
	metrics *Mockmetrics
	now     time.Time
	input   model.CreateTrip
}

func setup(t *testing.T) fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	f := fixture{
		repo:    NewMocktripRepository(ctrl),
		keys:    NewMockidempotencyRepository(ctrl),
		tx:      NewMocktxManager(ctrl),
		metrics: NewMockmetrics(ctrl),
		now:     time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
		input: model.CreateTrip{
			UserID:   uuid.New(),
			DriverID: uuid.New(),
			Price:    1450,
		},
	}

	f.u = create.New(f.repo, f.keys, f.tx, f.metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), 24*time.Hour, func() time.Time { return f.now })
	return f
}

func TestCreate(t *testing.T) {
	t.Parallel()

	for _, keyed := range []bool{false, true} {
		t.Run(fmt.Sprintf("key=%t", keyed), func(t *testing.T) {
			t.Parallel()

			f := setup(t)
			ctx := t.Context()
			var saved model.Trip
			committed := false
			f.tx.EXPECT().Do(ctx, gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
				err := fn(ctx)
				committed = err == nil
				return err
			})
			if keyed {
				key := uuid.New()
				f.input.IdempotencyKey = &key
				f.keys.EXPECT().DeleteExpired(ctx, key, f.now.Add(-24*time.Hour)).Return(nil)
				f.keys.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, record model.IdempotencyKey) error {
					require.Equal(t, key, record.Key)
					require.Equal(t, f.now, record.CreatedAt)
					require.Len(t, record.RequestHash, 64)
					return nil
				})
				f.keys.EXPECT().Attach(ctx, key, gomock.Any()).DoAndReturn(func(_ context.Context, _, id uuid.UUID) error { require.Equal(t, saved.ID, id); return nil })
			}

			f.repo.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, item model.Trip, change model.Transition) error {
				saved = item
				require.NotEqual(t, uuid.Nil, item.ID)
				require.Equal(t, f.input.UserID, item.UserID)
				require.Equal(t, f.input.DriverID, item.DriverID)
				require.Equal(t, f.input.Price, item.Price)
				require.Equal(t, f.now, item.StartedAt)
				require.Equal(t, trip.StatusACTIVE, item.Status)
				require.Nil(t, item.FinishedAt)
				require.Equal(t, model.Transition{
					From:   trip.StatusNONE,
					To:     trip.StatusACTIVE,
					At:     f.now,
					Reason: "created",
				}, change)
				return nil
			})
			f.metrics.EXPECT().Created(ctx).Do(func(context.Context) { require.True(t, committed) })
			got, err := f.u.Run(ctx, f.input)
			require.NoError(t, err)
			require.Equal(t, saved, got.Trip)
			require.False(t, got.Repeated)
		})
	}
}

func TestCreateFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		err        error
		repository bool
	}{
		{name: "driver busy", err: trip.ErrDriverBusy, repository: true},
		{name: "repository error", err: errors.New("storage unavailable"), repository: true},
		{name: "transaction error", err: errors.New("transaction failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := setup(t)
			ctx := t.Context()
			if tc.repository {
				f.tx.EXPECT().Do(ctx, gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
					return fn(ctx)
				})
				f.repo.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).Return(tc.err)
			} else {
				f.tx.EXPECT().Do(ctx, gomock.Any()).Return(tc.err)
			}

			got, err := f.u.Run(ctx, f.input)
			require.ErrorIs(t, err, tc.err)
			require.Empty(t, got)
		})
	}
}

func TestRepeat(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{"same hash", "same fields", conflictScenario, keyErrorScenario, tripErrorScenario} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			f := setup(t)
			ctx := t.Context()
			key, id := uuid.New(), uuid.New()
			f.input.IdempotencyKey = &key
			var hash string
			f.tx.EXPECT().Do(ctx, gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
			f.keys.EXPECT().DeleteExpired(ctx, key, gomock.Any()).Return(nil)
			f.keys.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, record model.IdempotencyKey) error {
				hash = record.RequestHash
				return fmt.Errorf("duplicate: %w", trip.ErrIdempotencyKeyExists)
			})
			failure := errors.New("read failure")
			f.keys.EXPECT().Get(ctx, key).DoAndReturn(func(_ context.Context, _ uuid.UUID) (model.IdempotencyKey, error) {
				if scenario == keyErrorScenario {
					return model.IdempotencyKey{}, failure
				}

				if scenario != "same hash" {
					hash = "previous representation"
				}

				return model.IdempotencyKey{Key: key, TripID: id, RequestHash: hash}, nil
			})
			entity, _, err := trip.New(id, f.input.UserID, f.input.DriverID, f.input.Start, f.input.End, f.input.Price, f.now)
			require.NoError(t, err)

			found := model.Trip(entity)
			if scenario == conflictScenario {
				found.Price++
			}

			if scenario != keyErrorScenario {
				var repoErr error
				if scenario == tripErrorScenario {
					repoErr = failure
				}

				f.repo.EXPECT().Get(ctx, id).Return(found, repoErr)
			}

			got, err := f.u.Run(ctx, f.input)
			switch scenario {
			case conflictScenario:
				require.ErrorIs(t, err, trip.ErrIdempotencyConflict)
			case keyErrorScenario, tripErrorScenario:
				require.ErrorIs(t, err, failure)
			default:
				require.NoError(t, err)
				require.True(t, got.Repeated)
				require.Equal(t, found, got.Trip)
			}
		})
	}
}

func TestIdempotencyHashIncludesRequestFields(t *testing.T) {
	t.Parallel()

	input := model.CreateTrip{
		UserID: uuid.New(), DriverID: uuid.New(), Price: 1250,
		Start: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		End:   trip.Coordinates{Latitude: 55.8, Longitude: 37.7},
	}

	original := hashForInput(t, input)
	for name, change := range map[string]func(*model.CreateTrip){
		"user":            func(v *model.CreateTrip) { v.UserID = uuid.New() },
		"driver":          func(v *model.CreateTrip) { v.DriverID = uuid.New() },
		"price":           func(v *model.CreateTrip) { v.Price++ },
		"start latitude":  func(v *model.CreateTrip) { v.Start.Latitude += 0.001 },
		"start longitude": func(v *model.CreateTrip) { v.Start.Longitude += 0.001 },
		"end latitude":    func(v *model.CreateTrip) { v.End.Latitude += 0.001 },
		"end longitude":   func(v *model.CreateTrip) { v.End.Longitude += 0.001 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := input
			change(&changed)
			require.NotEqual(t, original, hashForInput(t, changed))
		})
	}

	input.Start.Latitude = 0
	zero := hashForInput(t, input)
	input.Start.Latitude = math.Copysign(0, -1)
	require.Equal(t, zero, hashForInput(t, input))
}

func hashForInput(t *testing.T, input model.CreateTrip) string {
	t.Helper()

	f := setup(t)
	key := uuid.New()
	input.IdempotencyKey = &key
	ctx := t.Context()
	stop := errors.New("capture hash")
	var hash string
	f.tx.EXPECT().Do(ctx, gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	f.keys.EXPECT().DeleteExpired(ctx, key, gomock.Any()).Return(nil)
	f.keys.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, record model.IdempotencyKey) error {
		hash = record.RequestHash
		return stop
	})
	_, err := f.u.Run(ctx, input)
	require.ErrorIs(t, err, stop)
	return hash
}
