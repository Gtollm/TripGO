package list

import (
	"context"
	"fmt"

	"github.com/Gtollm/TripGO/internal/model"
)

type UseCase struct {
	trips     tripRepository
	positions positionRepository
}

func New(trips tripRepository, positions positionRepository) *UseCase {
	return &UseCase{
		trips:     trips,
		positions: positions,
	}
}

func (u *UseCase) Run(ctx context.Context, input model.ListPositions) (model.ListPositionsResult, error) {
	if _, err := u.trips.Get(ctx, input.TripID); err != nil {
		return model.ListPositionsResult{}, fmt.Errorf("get trip for positions: %w", err)
	}

	positions, err := u.positions.ListByTrip(ctx, input.TripID)
	if err != nil {
		return model.ListPositionsResult{}, fmt.Errorf("list positions: %w", err)
	}

	return model.ListPositionsResult{Positions: positions}, nil
}
