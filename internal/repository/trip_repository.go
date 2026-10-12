package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/Gtollm/TripGO/internal/domain/trip"
)

type TripRepository interface {
	Create(ctx context.Context, created trip.Trip, change trip.Transition) error
	Get(ctx context.Context, id uuid.UUID) (trip.Trip, error)
	GetForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error)
	UpdateStatus(ctx context.Context, finished trip.Trip, change trip.Transition) error
}
