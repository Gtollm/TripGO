//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=create_test

package create

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Gtollm/TripGO/internal/model"
)

type tripRepository interface {
	GetForUpdate(ctx context.Context, id uuid.UUID) (model.Trip, error)
	UpdateLastPositionAt(ctx context.Context, tripID uuid.UUID, recordedAt, updatedAt time.Time) error
}

type positionRepository interface {
	Create(ctx context.Context, position model.Position, createdAt time.Time) (model.Position, error)
}

type txManager interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}

type queueCanceller interface {
	Cancel(ctx context.Context, tripID uuid.UUID)
}

type metrics interface {
	PositionSaved(ctx context.Context)
}
