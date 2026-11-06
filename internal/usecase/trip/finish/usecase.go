package finish

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/model"
)

type UseCase struct {
	trips   tripRepository
	tx      txManager
	metrics metrics
	logger  *slog.Logger
	now     func() time.Time
}

func New(trips tripRepository, tx txManager, metrics metrics, logger *slog.Logger, now func() time.Time) *UseCase {
	return &UseCase{
		trips:   trips,
		tx:      tx,
		metrics: metrics,
		logger:  logger,
		now:     now,
	}
}

func (u *UseCase) Run(ctx context.Context, input model.FinishTrip) (model.FinishTripResult, error) {
	var finished model.Trip
	err := u.tx.Do(ctx, func(ctx context.Context) error {
		stored, err := u.trips.GetForUpdate(ctx, input.ID)
		if err != nil {
			return err
		}

		found := trip.Trip(stored)
		change, err := found.Finish(u.now())
		if err != nil {
			return err
		}

		if err := u.trips.UpdateStatus(ctx, model.Trip(found), model.Transition(change)); err != nil {
			return err
		}

		finished = model.Trip(found)
		return nil
	})
	if err != nil {
		return model.FinishTripResult{}, fmt.Errorf("finish trip: %w", err)
	}

	u.metrics.Completed(ctx)
	u.logger.InfoContext(ctx, "trip completed", "trip_id", finished.ID.String())
	return model.FinishTripResult{Trip: finished}, nil
}
