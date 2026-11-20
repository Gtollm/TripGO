package trip

import (
	"time"

	"github.com/google/uuid"
)

type Coordinates struct {
	Latitude  float64
	Longitude float64
}

type Trip struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	DriverID       uuid.UUID
	Start          Coordinates
	End            Coordinates
	Price          int64
	Status         Status
	StartedAt      time.Time
	FinishedAt     *time.Time
	LastPositionAt *time.Time
}

type Transition struct {
	From   Status
	To     Status
	At     time.Time
	Reason string
}

func New(
	id, userID, driverID uuid.UUID,
	start, end Coordinates,
	price int64,
	at time.Time,
) (Trip, Transition, error) {
	if err := validateID("id", id); err != nil {
		return Trip{}, Transition{}, err
	}

	if err := validateID("user_id", userID); err != nil {
		return Trip{}, Transition{}, err
	}

	if err := validateID("driver_id", driverID); err != nil {
		return Trip{}, Transition{}, err
	}

	if err := validateCoordinates("start_point", start); err != nil {
		return Trip{}, Transition{}, err
	}

	if err := validateCoordinates("end_point", end); err != nil {
		return Trip{}, Transition{}, err
	}

	if err := validatePrice(price); err != nil {
		return Trip{}, Transition{}, err
	}

	if err := validateTime("started_at", at); err != nil {
		return Trip{}, Transition{}, err
	}

	created := Trip{
		ID:        id,
		UserID:    userID,
		DriverID:  driverID,
		Start:     start,
		End:       end,
		Price:     price,
		Status:    StatusACTIVE,
		StartedAt: at,
	}

	transition := Transition{
		From:   StatusNONE,
		To:     StatusACTIVE,
		At:     at,
		Reason: "created",
	}

	return created, transition, nil
}

func (t *Trip) EnsureActive() error {
	switch t.Status {
	case StatusACTIVE:
		return nil
	case StatusCOMPLETED:
		return ErrTripCompleted
	default:
		return &ValidationError{Field: "status", Reason: "must be active"}
	}
}

func (t *Trip) Finish(at time.Time) (Transition, error) {
	if err := t.EnsureActive(); err != nil {
		return Transition{}, err
	}

	if err := validateTime("finished_at", at); err != nil {
		return Transition{}, err
	}

	if err := validateTimeAfter("finished_at", at, t.StartedAt); err != nil {
		return Transition{}, err
	}

	t.Status = StatusCOMPLETED
	t.FinishedAt = &at
	return Transition{
		From:   StatusACTIVE,
		To:     StatusCOMPLETED,
		At:     at,
		Reason: "finished",
	}, nil
}
