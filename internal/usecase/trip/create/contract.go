//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=create_test

package create

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Gtollm/TripGO/internal/model"
)

type tripRepository interface {
	Create(ctx context.Context, created model.Trip, change model.Transition) error
	Get(ctx context.Context, id uuid.UUID) (model.Trip, error)
}

type idempotencyRepository interface {
	DeleteExpired(ctx context.Context, key uuid.UUID, expiredBefore time.Time) error
	Create(ctx context.Context, key model.IdempotencyKey) error
	Attach(ctx context.Context, key, tripID uuid.UUID) error
	Get(ctx context.Context, key uuid.UUID) (model.IdempotencyKey, error)
}

type txManager interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}

type metrics interface {
	Created(ctx context.Context)
}
