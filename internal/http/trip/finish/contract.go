//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=finish_test

package finish

import (
	"context"

	"github.com/Gtollm/TripGO/internal/model"
)

type usecase interface {
	Run(ctx context.Context, input model.FinishTrip) (model.FinishTripResult, error)
}
