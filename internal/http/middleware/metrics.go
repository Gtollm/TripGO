package middleware

import (
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/Gtollm/TripGO/internal/http/requeststate"
)

type Metrics struct {
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
}

func NewMetrics(meter metric.Meter) (*Metrics, error) {
	duration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithUnit("s"),
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
		attrs := []attribute.KeyValue{attribute.String("http.request.method", r.Method), attribute.String("http.route", state.Route)}
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
