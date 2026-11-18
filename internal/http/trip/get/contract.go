//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=get_test

package get

import (
	"context"

	"github.com/Gtollm/TripGO/internal/model"
)

type usecase interface {
	Run(ctx context.Context, input model.GetTrip) (model.GetTripResult, error)
}
