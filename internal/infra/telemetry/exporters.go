package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/Gtollm/TripGO/internal/config"
)

const (
	grpcProtocol = "grpc"
)

type exporters struct {
	traces  sdktrace.SpanExporter
	metrics sdkmetric.Exporter
}

func newExporters(ctx context.Context, cfg config.TelemetryConfig) (result exporters, err error) {
	defer func() {
		if err != nil {
			err = errors.Join(err, result.shutdown(ctx))
		}
	}()
	traceEndpoint, err := url.JoinPath(cfg.Endpoint, "v1", "traces")
	if err != nil {
		return result, fmt.Errorf("trace endpoint: %w", err)
	}

	metricEndpoint, err := url.JoinPath(cfg.Endpoint, "v1", "metrics")
	if err != nil {
		return result, fmt.Errorf("metric endpoint: %w", err)
	}

	if cfg.Protocol == grpcProtocol {
		result.traces, err = otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(cfg.Endpoint))
	} else {
		result.traces, err = otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(traceEndpoint))
	}

	if err != nil {
		return result, fmt.Errorf("trace exporter: %w", err)
	}

	if cfg.Protocol == grpcProtocol {
		result.metrics, err = otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL(cfg.Endpoint))
	} else {
		result.metrics, err = otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(metricEndpoint))
	}

	if err != nil {
		return result, fmt.Errorf("metric exporter: %w", err)
	}

	return result, nil
}

func (e exporters) shutdown(ctx context.Context) error {
	var err error
	if e.traces != nil {
		err = errors.Join(err, e.traces.Shutdown(ctx))
	}

	if e.metrics != nil {
		err = errors.Join(err, e.metrics.Shutdown(ctx))
	}

	return err
}
