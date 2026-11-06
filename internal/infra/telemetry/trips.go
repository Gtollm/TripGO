package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

type TripMetrics struct {
	created      metric.Int64Counter
	completed    metric.Int64Counter
	registration metric.Registration
}

func NewTripMetrics(meter metric.Meter, trips activeTrips) (*TripMetrics, error) {
	created, err := meter.Int64Counter("tripgo.trips.created", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("created metric: %w", err)
	}

	completed, err := meter.Int64Counter("tripgo.trips.completed", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("completed metric: %w", err)
	}

	active, err := meter.Int64ObservableGauge("tripgo.trips.active", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("active metric: %w", err)
	}

	registration, err := meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		count, err := trips.CountActive(ctx)
		if err != nil {
			return fmt.Errorf("observe active trips: %w", err)
		}

		observer.ObserveInt64(active, count)
		return nil
	}, active)
	if err != nil {
		return nil, fmt.Errorf("register active trips: %w", err)
	}

	return &TripMetrics{
		created:      created,
		completed:    completed,
		registration: registration,
	}, nil
}

func (m *TripMetrics) Created(ctx context.Context)   { m.created.Add(ctx, 1) }
func (m *TripMetrics) Completed(ctx context.Context) { m.completed.Add(ctx, 1) }
func (m *TripMetrics) Close() error                  { return m.registration.Unregister() }
