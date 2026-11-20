package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/Gtollm/TripGO/internal/domain/trip"
)

type CreatePosition struct {
	TripID      uuid.UUID
	Coordinates trip.Coordinates
	RecordedAt  time.Time
}

type CreatePositionResult struct {
	Position Position
}

type ListPositions struct {
	TripID uuid.UUID
}

type ListPositionsResult struct {
	Positions []Position
}

type Position struct {
	ID          int64
	TripID      uuid.UUID
	Coordinates trip.Coordinates
	RecordedAt  time.Time
}
