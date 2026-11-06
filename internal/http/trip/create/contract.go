//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=create_test

package create

import (
	"context"

	"github.com/Gtollm/TripGO/internal/model"
)

type usecase interface {
	Run(ctx context.Context, input model.CreateTrip) (model.CreatedTrip, error)
}
