package trip

import (
	"time"

	"github.com/google/uuid"
)

type Position struct {
	ID          int64
	TripID      uuid.UUID
	Coordinates Coordinates
	RecordedAt  time.Time
}

func NewPosition(tripID uuid.UUID, coordinates Coordinates, recordedAt, now time.Time) (Position, error) {
	if err := validateID("trip_id", tripID); err != nil {
		return Position{}, err
	}

	if err := validateCoordinates("coordinates", coordinates); err != nil {
		return Position{}, err
	}

	if err := validateTime("recorded_at", recordedAt); err != nil {
		return Position{}, err
	}

	if recordedAt.After(now.Add(time.Minute)) {
		return Position{}, &ValidationError{
			Field:  "recorded_at",
			Reason: "must not be more than 1 minute in the future",
		}
	}

	return Position{
		TripID:      tripID,
		Coordinates: coordinates,
		RecordedAt:  recordedAt,
	}, nil
}
