package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/Gtollm/TripGO/internal/http/requeststate"
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

type Metrics struct {
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
}

func NewMetrics(meter metric.Meter) (*Metrics, error) {
	duration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			bucket5ms, bucket10ms, bucket25ms, bucket50ms, bucket75ms, bucket100ms,
			bucket250ms, bucket500ms, bucket750ms, bucket1s, bucket2500ms, bucket5s, bucket7500ms, bucket10s,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("http duration metric: %w", err)
	}

	active, err := meter.Int64UpDownCounter("http.server.active_requests", metric.WithUnit("{request}"))
	if err != nil {
		return nil, fmt.Errorf("http active metric: %w", err)
	}

	return &Metrics{duration: duration, active: active}, nil
}

func (m *Metrics) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := requeststate.From(r.Context())
		attrs := []attribute.KeyValue{
			attribute.String("http.request.method", metricMethod(r.Method)),
			attribute.String("http.route", state.Route),
		}
		m.active.Add(r.Context(), 1, metric.WithAttributes(attrs...))
		state.Complete = append(state.Complete, func() {
			m.active.Add(state.Context, -1, metric.WithAttributes(attrs...))
			m.duration.Record(state.Context, time.Since(state.Started).Seconds(), metric.WithAttributes(
				attrs[0], attrs[1], attribute.Int("http.response.status_code", state.Status()),
			))
		})
		next.ServeHTTP(w, r)
	})
}

func metricMethod(method string) string {
	switch method = strings.ToUpper(method); method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodConnect, http.MethodOptions, http.MethodTrace, http.MethodPatch:
		return method
	default:
		return "_OTHER"
	}
}
