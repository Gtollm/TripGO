//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=positions_test

package positions

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Gtollm/TripGO/internal/model"
)

type staleTripsRepository interface {
	ListStaleBatch(ctx context.Context, staleAfter time.Duration, cursor *model.StaleCursor, limit int) ([]model.StaleTrip, error)
}

type pushClient interface {
	RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error
}

type metrics interface {
	Observe(queueLen func() int, activeWorkers func() int64)
	StaleCheck(ctx context.Context, result string)
	RowsRead(ctx context.Context, count int64)
	RequestCancelled(ctx context.Context)
	RequestCompleted(ctx context.Context, result string, durationSeconds float64)
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}
