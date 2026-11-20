package create

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/model"
)

type UseCase struct {
	trips     tripRepository
	positions positionRepository
	tx        txManager
	canceller queueCanceller
	metrics   metrics
	logger    *slog.Logger
	now       func() time.Time
}

func New(
	trips tripRepository,
	positions positionRepository,
	tx txManager,
	canceller queueCanceller,
	metrics metrics,
	logger *slog.Logger,
	now func() time.Time,
) *UseCase {
	return &UseCase{
		trips:     trips,
		positions: positions,
		tx:        tx,
		canceller: canceller,
		metrics:   metrics,
		logger:    logger,
		now:       now,
	}
}

func (u *UseCase) Run(ctx context.Context, input model.CreatePosition) (model.CreatePositionResult, error) {
	now := u.now()
	entity, err := trip.NewPosition(input.TripID, input.Coordinates, input.RecordedAt, now)
	if err != nil {
		return model.CreatePositionResult{}, fmt.Errorf("create position: %w", err)
	}

	var saved model.Position
	err = u.tx.Do(ctx, func(ctx context.Context) error {
		stored, err := u.trips.GetForUpdate(ctx, input.TripID)
		if err != nil {
			return err
		}

		found := trip.Trip(stored)
		if err := found.EnsureActive(); err != nil {
			return err
		}

		saved, err = u.positions.Create(ctx, model.Position(entity), now)
		if err != nil {
			return err
		}

		return u.trips.UpdateLastPositionAt(ctx, input.TripID, saved.RecordedAt, now)
	})
	if err != nil {
		return model.CreatePositionResult{}, fmt.Errorf("create position: %w", err)
	}

	u.canceller.Cancel(ctx, input.TripID)
	u.metrics.PositionSaved(ctx)
	u.logger.DebugContext(ctx, "position saved", "trip_id", saved.TripID.String(), "position_id", saved.ID)
	return model.CreatePositionResult{Position: saved}, nil
}
