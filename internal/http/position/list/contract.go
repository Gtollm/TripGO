//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=list_test

package list

import (
	"context"

	"github.com/Gtollm/TripGO/internal/model"
)

type usecase interface {
	Run(ctx context.Context, input model.ListPositions) (model.ListPositionsResult, error)
}
