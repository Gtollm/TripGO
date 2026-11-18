package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/Gtollm/TripGO/internal/domain/trip"
)

type CreateTrip struct {
	IdempotencyKey *uuid.UUID
	UserID         uuid.UUID
	DriverID       uuid.UUID
	Start          trip.Coordinates
	End            trip.Coordinates
	Price          int64
}

type CreatedTrip struct {
	Trip     Trip
	Repeated bool
}

type GetTrip struct {
	ID uuid.UUID
}

type GetTripResult struct {
	Trip Trip
}

type FinishTrip struct {
	ID uuid.UUID
}

type FinishTripResult struct {
	Trip Trip
}

type Trip struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DriverID   uuid.UUID
	Start      trip.Coordinates
	End        trip.Coordinates
	Price      int64
	Status     trip.Status
	StartedAt  time.Time
	FinishedAt *time.Time
}

type Transition struct {
	From   trip.Status
	To     trip.Status
	At     time.Time
	Reason string
}

type IdempotencyKey struct {
	Key         uuid.UUID
	RequestHash string
	TripID      uuid.UUID
	CreatedAt   time.Time
}
