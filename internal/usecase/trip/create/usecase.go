package create

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/model"
)

type UseCase struct {
	trips   tripRepository
	keys    idempotencyRepository
	tx      txManager
	metrics metrics
	logger  *slog.Logger
	keyTTL  time.Duration
	now     func() time.Time
}

func New(trips tripRepository, keys idempotencyRepository, tx txManager, metrics metrics,
	logger *slog.Logger, keyTTL time.Duration, now func() time.Time,
) *UseCase {
	return &UseCase{
		trips:   trips,
		keys:    keys,
		tx:      tx,
		metrics: metrics,
		logger:  logger,
		keyTTL:  keyTTL,
		now:     now,
	}
}

func (u *UseCase) Run(ctx context.Context, input model.CreateTrip) (model.CreatedTrip, error) {
	now := u.now()
	entity, transition, err := trip.New(uuid.New(), input.UserID, input.DriverID, input.Start, input.End, input.Price, now)
	if err != nil {
		return model.CreatedTrip{}, fmt.Errorf("create trip: %w", err)
	}

	created := model.Trip(entity)
	change := model.Transition(transition)
	hash := requestHash(created)
	err = u.tx.Do(ctx, func(ctx context.Context) error {
		if input.IdempotencyKey == nil {
			return u.trips.Create(ctx, created, change)
		}

		key := *input.IdempotencyKey
		if err := u.keys.DeleteExpired(ctx, key, now.Add(-u.keyTTL)); err != nil {
			return err
		}

		if err := u.keys.Create(ctx, model.IdempotencyKey{Key: key, RequestHash: hash, CreatedAt: now}); err != nil {
			return err
		}

		if err := u.trips.Create(ctx, created, change); err != nil {
			return err
		}

		return u.keys.Attach(ctx, key, created.ID)
	})

	if input.IdempotencyKey != nil && errors.Is(err, trip.ErrIdempotencyKeyExists) {
		found, err := u.findRepeated(ctx, *input.IdempotencyKey, hash)
		if err != nil {
			return model.CreatedTrip{}, fmt.Errorf("repeat create trip: %w", err)
		}

		return model.CreatedTrip{Trip: found, Repeated: true}, nil
	}

	if err != nil {
		return model.CreatedTrip{}, fmt.Errorf("create trip: %w", err)
	}

	u.metrics.Created(ctx)
	u.logger.InfoContext(ctx, "trip created", "trip_id", created.ID.String())
	return model.CreatedTrip{Trip: created}, nil
}

func (u *UseCase) findRepeated(ctx context.Context, key uuid.UUID, hash string) (model.Trip, error) {
	stored, err := u.keys.Get(ctx, key)
	if err != nil {
		return model.Trip{}, err
	}

	found, err := u.trips.Get(ctx, stored.TripID)
	if err != nil {
		return model.Trip{}, err
	}

	if stored.RequestHash != hash && requestHash(found) != hash {
		return model.Trip{}, trip.ErrIdempotencyConflict
	}

	return found, nil
}
