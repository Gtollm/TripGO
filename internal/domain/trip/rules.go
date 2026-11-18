package trip

import (
	"math"
	"time"

	"github.com/google/uuid"
)

func validateID(field string, id uuid.UUID) error {
	if id == uuid.Nil {
		return &ValidationError{Field: field, Reason: "must not be empty"}
	}
	return nil
}

func validatePrice(price int64) error {
	if price < 0 {
		return &ValidationError{Field: "price", Reason: "must be greater than or equal to zero"}
	}
	return nil
}

func validateTime(field string, at time.Time) error {
	if at.IsZero() {
		return &ValidationError{Field: field, Reason: "must not be empty"}
	}
	return nil
}

func validateTimeAfter(field string, at, earliest time.Time) error {
	if at.Before(earliest) {
		return &ValidationError{Field: field, Reason: "must not be before started_at"}
	}
	return nil
}

func validateCoordinates(field string, c Coordinates) error {
	if math.IsNaN(c.Latitude) || c.Latitude < -90 || c.Latitude > 90 {
		return &ValidationError{Field: field + ".latitude", Reason: "out of range"}
	}
	if math.IsNaN(c.Longitude) || c.Longitude < -180 || c.Longitude > 180 {
		return &ValidationError{Field: field + ".longitude", Reason: "out of range"}
	}
	return nil
}
