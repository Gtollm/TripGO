package telemetry

import (
	"context"
	"fmt"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	bucket5ms    = 0.005
	bucket10ms   = 0.01
	bucket25ms   = 0.025
	bucket50ms   = 0.05
	bucket75ms   = 0.075
	bucket100ms  = 0.1
	bucket250ms  = 0.25
	bucket500ms  = 0.5
	bucket750ms  = 0.75
	bucket1s     = 1
	bucket2500ms = 2.5
	bucket5s     = 5
	bucket7500ms = 7.5
	bucket10s    = 10
)

type PositionMetrics struct {
	staleChecks   metric.Int64Counter
	rowsRead      metric.Int64Histogram
	requests      metric.Int64Counter
	duration      metric.Float64Histogram
	registration  metric.Registration
	queueLen      atomic.Pointer[func() int]
	activeWorkers atomic.Pointer[func() int64]
}

func NewPositionMetrics(meter metric.Meter) (*PositionMetrics, error) {
	staleChecks, err := meter.Int64Counter("tripgo.stale_checks", metric.WithUnit("{check}"))
	if err != nil {
		return nil, fmt.Errorf("stale checks metric: %w", err)
	}

	rowsRead, err := meter.Int64Histogram("tripgo.stale_checks.rows_read", metric.WithUnit("{row}"))
	if err != nil {
		return nil, fmt.Errorf("stale checks rows read metric: %w", err)
	}

	queueSize, err := meter.Int64ObservableGauge("tripgo.position_requests.queue.size", metric.WithUnit("{request}"))
	if err != nil {
		return nil, fmt.Errorf("queue size metric: %w", err)
	}

	workersActive, err := meter.Int64ObservableGauge("tripgo.position_requests.workers.active", metric.WithUnit("{worker}"))
	if err != nil {
		return nil, fmt.Errorf("workers active metric: %w", err)
	}

	requests, err := meter.Int64Counter("tripgo.position_requests", metric.WithUnit("{request}"))
	if err != nil {
		return nil, fmt.Errorf("position requests metric: %w", err)
	}

	duration, err := meter.Float64Histogram(
		"tripgo.position_requests.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			bucket5ms, bucket10ms, bucket25ms, bucket50ms, bucket75ms, bucket100ms,
			bucket250ms, bucket500ms, bucket750ms, bucket1s, bucket2500ms, bucket5s, bucket7500ms, bucket10s,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("position requests duration metric: %w", err)
	}

	m := &PositionMetrics{
		staleChecks: staleChecks,
		rowsRead:    rowsRead,
		requests:    requests,
		duration:    duration,
	}

	registration, err := meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		if fn := m.queueLen.Load(); fn != nil {
			observer.ObserveInt64(queueSize, int64((*fn)()))
		}
		if fn := m.activeWorkers.Load(); fn != nil {
			observer.ObserveInt64(workersActive, (*fn)())
		}
		return nil
	}, queueSize, workersActive)
	if err != nil {
		return nil, fmt.Errorf("register position worker gauges: %w", err)
	}

	m.registration = registration
	return m, nil
}

func (m *PositionMetrics) Observe(queueLen func() int, activeWorkers func() int64) {
	m.queueLen.Store(&queueLen)
	m.activeWorkers.Store(&activeWorkers)
}

func (m *PositionMetrics) StaleCheck(ctx context.Context, result string) {
	m.staleChecks.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

func (m *PositionMetrics) RowsRead(ctx context.Context, count int64) {
	m.rowsRead.Record(ctx, count)
}

func (m *PositionMetrics) RequestCancelled(ctx context.Context) {
	m.requests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "cancelled")))
}

func (m *PositionMetrics) RequestCompleted(ctx context.Context, result string, durationSeconds float64) {
	attr := metric.WithAttributes(attribute.String("result", result))
	m.requests.Add(ctx, 1, attr)
	m.duration.Record(ctx, durationSeconds, attr)
}

func (m *PositionMetrics) Close() error {
	return m.registration.Unregister()
}
