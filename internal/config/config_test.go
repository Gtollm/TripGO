package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Gtollm/TripGO/internal/config"
)

const (
	maxConnsEnv  = "DATABASE_MAX_CONNS"
	invalidValue = "invalid"
)

func environment(t *testing.T) {
	t.Helper()

	for name, value := range map[string]string{
		"HTTP_ADDR":                   ":8080",
		"LOG_LEVEL":                   "info",
		"SHUTDOWN_TIMEOUT":            "10s",
		"IDEMPOTENCY_KEY_TTL":         "24h",
		"HTTP_READ_HEADER_TIMEOUT":    "5s",
		"HTTP_READ_TIMEOUT":           "10s",
		"HTTP_WRITE_TIMEOUT":          "15s",
		"HTTP_IDLE_TIMEOUT":           "60s",
		"DATABASE_URL":                "postgres://localhost/tripgo",
		maxConnsEnv:                   "10",
		"DATABASE_MIN_CONNS":          "2",
		"DATABASE_MAX_CONN_LIFETIME":  "30m",
		"DATABASE_CONNECT_TIMEOUT":    "5s",
		"DATABASE_QUERY_TIMEOUT":      "3s",
		"OTEL_SERVICE_NAME":           "trip-service",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318",
		"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
		"OTEL_TRACES_SAMPLER":         "parentbased_always_on",
		"OTEL_TRACES_SAMPLER_ARG":     "",
	} {
		t.Setenv(name, value)
	}
}

func TestLoad(t *testing.T) {
	environment(t)

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour, cfg.IdempotencyKeyTTL)
	require.EqualValues(t, 10, cfg.DatabaseConfig.MaxConnections)
	require.Equal(t, "http/protobuf", cfg.Telemetry.Protocol)

	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	t.Setenv("OTEL_TRACES_SAMPLER", "parentbased_traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")

	cfg, err = config.Load()
	require.NoError(t, err)
	require.Equal(t, 0.25, cfg.Telemetry.SamplerRatio)
}

func TestRejectInvalidConfig(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"DATABASE_URL", ""},
		{maxConnsEnv, "0"},
		{maxConnsEnv, "2147483648"},
		{"DATABASE_MIN_CONNS", "11"},
		{"DATABASE_QUERY_TIMEOUT", "0s"},
		{"HTTP_READ_TIMEOUT", invalidValue},
		{"HTTP_ADDR", ""},
		{"SHUTDOWN_TIMEOUT", "-1s"},
		{"OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4318"},
		{"OTEL_EXPORTER_OTLP_PROTOCOL", invalidValue},
		{"OTEL_TRACES_SAMPLER", invalidValue},
		{"OTEL_SERVICE_NAME", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			environment(t)
			t.Setenv(tc.name, tc.value)

			_, err := config.Load()
			require.ErrorContains(t, err, tc.name)
		})
	}

	for _, ratio := range []string{"NaN", "Inf", "-0.1", "1.1", invalidValue} {
		t.Run(ratio, func(t *testing.T) {
			environment(t)
			t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", ratio)

			_, err := config.Load()
			require.ErrorContains(t, err, "OTEL_TRACES_SAMPLER_ARG")
		})
	}
}
