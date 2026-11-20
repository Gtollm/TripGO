package convert

import (
	api "github.com/Gtollm/TripGO/internal/generated"
	"github.com/Gtollm/TripGO/internal/model"
)

func ToAPI(item model.Position) api.TripPosition {
	return api.TripPosition{
		Id:     item.ID,
		TripId: item.TripID,
		Coordinates: api.Coordinates{
			Latitude:  item.Coordinates.Latitude,
			Longitude: item.Coordinates.Longitude,
		},
		RecordedAt: item.RecordedAt,
	}
}

func ToListAPI(items []model.Position) api.TripPositionsResponse {
	positions := make([]api.TripPosition, 0, len(items))
	for _, item := range items {
		positions = append(positions, ToAPI(item))
	}

	return api.TripPositionsResponse{Positions: positions}
}
