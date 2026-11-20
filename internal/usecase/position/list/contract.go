//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=list_test

package list

import (
	"context"

	"github.com/google/uuid"

	"github.com/Gtollm/TripGO/internal/model"
)

type tripRepository interface {
	Get(ctx context.Context, id uuid.UUID) (model.Trip, error)
}

type positionRepository interface {
	ListByTrip(ctx context.Context, tripID uuid.UUID) ([]model.Position, error)
}
