package trip

import (
	"errors"
	"fmt"
)

var (
	ErrTripNotFound         = errors.New("trip not found")
	ErrTripCompleted        = errors.New("trip already completed")
	ErrDriverBusy           = errors.New("driver already has an active trip")
	ErrIdempotencyConflict  = errors.New("idempotency key was already used with a different request")
	ErrIdempotencyKeyExists = errors.New("idempotency key already exists")
)

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}
