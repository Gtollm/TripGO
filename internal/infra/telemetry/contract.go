package telemetry

import "context"

type activeTrips interface {
	CountActive(ctx context.Context) (int64, error)
}
