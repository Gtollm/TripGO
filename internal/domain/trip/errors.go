package trip

import (
	"errors"
	"fmt"
)

var (
	ErrTripNotFound  = errors.New("trip not found")
	ErrTripCompleted = errors.New("trip already completed")
	ErrDriverBusy    = errors.New("driver already has an active trip")
)

type TripGoError struct {
	Field  string
	Reason string
}

func (e *TripGoError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}
