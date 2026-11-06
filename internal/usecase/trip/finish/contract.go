//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=finish_test

package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/Gtollm/TripGO/internal/model"
)

type tripRepository interface {
	GetForUpdate(ctx context.Context, id uuid.UUID) (model.Trip, error)
	UpdateStatus(ctx context.Context, finished model.Trip, change model.Transition) error
}

type txManager interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}

type metrics interface{ Completed(ctx context.Context) }
