package get

import (
	"context"
	"fmt"

	"github.com/Gtollm/TripGO/internal/model"
)

type UseCase struct{ trips tripRepository }

func New(trips tripRepository) *UseCase { return &UseCase{trips: trips} }

func (u *UseCase) Run(ctx context.Context, input model.GetTrip) (model.GetTripResult, error) {
	found, err := u.trips.Get(ctx, input.ID)
	if err != nil {
		return model.GetTripResult{}, fmt.Errorf("get trip: %w", err)
	}

	return model.GetTripResult{Trip: found}, nil
}
