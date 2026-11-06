package convert

import (
	"fmt"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	api "github.com/Gtollm/TripGO/internal/generated"
	"github.com/Gtollm/TripGO/internal/model"
)

func ToAPI(item model.Trip) (api.Trip, error) {
	var status api.TripStatus
	switch item.Status {
	case trip.StatusACTIVE:
		status = api.Active
	case trip.StatusCOMPLETED:
		status = api.Completed
	default:
		return api.Trip{}, fmt.Errorf("unexpected trip status %q", item.Status)
	}

	return api.Trip{
		Id:         item.ID,
		UserId:     item.UserID,
		DriverId:   item.DriverID,
		StartPoint: api.Coordinates{Latitude: item.Start.Latitude, Longitude: item.Start.Longitude},
		EndPoint:   api.Coordinates{Latitude: item.End.Latitude, Longitude: item.End.Longitude},
		Price:      item.Price,
		Status:     status,
		StartedAt:  item.StartedAt,
		FinishedAt: item.FinishedAt,
	}, nil
}
