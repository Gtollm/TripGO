package telemetry

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"
)

type TripMetrics struct {
	tripsCreated   metric.Int64Counter
	tripsCompleted metric.Int64Counter
	positionsSaved metric.Int64Counter
	registration   metric.Registration
}

func NewTripMetrics(meter metric.Meter, trips activeTrips, logger *slog.Logger) (*TripMetrics, error) {
	tripsCreated, err := meter.Int64Counter("tripgo.trips.created", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("created metric: %w", err)
	}

	tripsCompleted, err := meter.Int64Counter("tripgo.trips.completed", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("completed metric: %w", err)
	}

	positionsSaved, err := meter.Int64Counter("tripgo.positions.saved", metric.WithUnit("{position}"))
	if err != nil {
		return nil, fmt.Errorf("positions saved metric: %w", err)
	}

	tripsActive, err := meter.Int64ObservableGauge("tripgo.trips.active", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("active metric: %w", err)
	}

	registration, err := meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		count, err := trips.CountActive(ctx)
		if err != nil {
			logger.WarnContext(ctx, "observe active trips failed", "error", err)
			return nil
		}

		observer.ObserveInt64(tripsActive, count)
		return nil
	}, tripsActive)
	if err != nil {
		return nil, fmt.Errorf("register active trips: %w", err)
	}

	return &TripMetrics{
		tripsCreated:   tripsCreated,
		tripsCompleted: tripsCompleted,
		positionsSaved: positionsSaved,
		registration:   registration,
	}, nil
}

func (m *TripMetrics) Created(ctx context.Context)       { m.tripsCreated.Add(ctx, 1) }
func (m *TripMetrics) Completed(ctx context.Context)     { m.tripsCompleted.Add(ctx, 1) }
func (m *TripMetrics) PositionSaved(ctx context.Context) { m.positionsSaved.Add(ctx, 1) }
func (m *TripMetrics) Close() error                      { return m.registration.Unregister() }
