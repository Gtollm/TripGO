package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/Gtollm/TripGO/internal/config"
)

type Telemetry struct {
	Logger  *slog.Logger
	Traces  *sdktrace.TracerProvider
	Metrics *sdkmetric.MeterProvider
}

func New(ctx context.Context, cfg config.TelemetryConfig, rawLevel string, output io.Writer) (*Telemetry, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(rawLevel)); err != nil {
		return nil, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	stdout := NewLogger(output, level).With("service", cfg.ServiceName)
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) { stdout.WarnContext(ctx, "telemetry export failed", "error", err) }))
	res, err := resource.New(ctx, resource.WithTelemetrySDK(), resource.WithAttributes(
		attribute.String("service.name", cfg.ServiceName), attribute.String("service.version", buildVersion()),
		attribute.String("service.namespace", "tripgo"), attribute.String("deployment.environment", "local"),
	), resource.WithFromEnv())
	if err != nil {
		return nil, fmt.Errorf("telemetry resource: %w", err)
	}

	exporters, err := newExporters(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return &Telemetry{
		Logger:  stdout,
		Traces:  sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithSampler(sampler(cfg)), sdktrace.WithBatcher(exporters.traces)),
		Metrics: sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporters.metrics))),
	}, nil
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	return errors.Join(t.Metrics.Shutdown(ctx), t.Traces.Shutdown(ctx))
}

func sampler(cfg config.TelemetryConfig) sdktrace.Sampler {
	switch cfg.Sampler {
	case "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(cfg.SamplerRatio)
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplerRatio))
	default:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}

		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}

	return "dev"
}
