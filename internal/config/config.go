package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	LogLevel          string
	ShutdownTimeout   time.Duration
	IdempotencyKeyTTL time.Duration

	DatabaseConfig DatabaseConfig
	HttpConfig     HttpConfig
	Telemetry      TelemetryConfig
}

type DatabaseConfig struct {
	URL                   string
	MaxConnections        int32
	MinConnections        int32
	MaxConnectionLifetime time.Duration
	ConnectTimeout        time.Duration
	QueryTimeout          time.Duration
}

type HttpConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

type TelemetryConfig struct {
	ServiceName  string
	Endpoint     string
	Protocol     string
	Sampler      string
	SamplerRatio float64
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := loadFromEnv(cfg); err != nil {
		return nil, err
	}

	var err error
	cfg.Telemetry, err = loadTelemetry()
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func loadFromEnv(cfg *Config) error {
	logLevel, err := requiredEnv("LOG_LEVEL")
	if err != nil {
		return err
	}

	cfg.LogLevel = logLevel

	cfg.ShutdownTimeout, err = requiredDuration("SHUTDOWN_TIMEOUT")
	if err != nil {
		return err
	}

	cfg.IdempotencyKeyTTL, err = requiredDuration("IDEMPOTENCY_KEY_TTL")
	if err != nil {
		return err
	}

	if err := loadDatabaseCfgFromEnv(&cfg.DatabaseConfig); err != nil {
		return err
	}

	return loadHttpCfgFromEnv(&cfg.HttpConfig)
}

func loadDatabaseCfgFromEnv(cfg *DatabaseConfig) error {
	dbURL, err := requiredEnv("DATABASE_URL")
	if err != nil {
		return err
	}

	cfg.URL = dbURL

	cfg.MaxConnections, err = requiredPositiveInt32("DATABASE_MAX_CONNS")
	if err != nil {
		return err
	}

	cfg.MinConnections, err = requiredPositiveInt32("DATABASE_MIN_CONNS")
	if err != nil {
		return err
	}

	if cfg.MinConnections > cfg.MaxConnections {
		return fmt.Errorf("DATABASE_MIN_CONNS must not be greater than DATABASE_MAX_CONNS")
	}

	cfg.MaxConnectionLifetime, err = requiredDuration("DATABASE_MAX_CONN_LIFETIME")
	if err != nil {
		return err
	}

	cfg.ConnectTimeout, err = requiredDuration("DATABASE_CONNECT_TIMEOUT")
	if err != nil {
		return err
	}

	cfg.QueryTimeout, err = requiredDuration("DATABASE_QUERY_TIMEOUT")
	if err != nil {
		return err
	}

	return nil
}

func loadHttpCfgFromEnv(cfg *HttpConfig) error {
	httpAddr, err := requiredEnv("HTTP_ADDR")
	if err != nil {
		return err
	}

	cfg.Addr = httpAddr

	cfg.ReadHeaderTimeout, err = requiredDuration("HTTP_READ_HEADER_TIMEOUT")
	if err != nil {
		return err
	}
	cfg.ReadTimeout, err = requiredDuration("HTTP_READ_TIMEOUT")
	if err != nil {
		return err
	}
	cfg.WriteTimeout, err = requiredDuration("HTTP_WRITE_TIMEOUT")
	if err != nil {
		return err
	}
	cfg.IdleTimeout, err = requiredDuration("HTTP_IDLE_TIMEOUT")
	if err != nil {
		return err
	}

	return nil
}

func requiredEnv(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s environment variable is required", name)
	}

	return value, nil
}

func requiredDuration(name string) (time.Duration, error) {
	raw, err := requiredEnv(name)
	if err != nil {
		return 0, err
	}

	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s should be a duration: %w", name, err)
	}

	if d <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}

	return d, nil
}

func requiredPositiveInt32(name string) (int32, error) {
	raw, err := requiredEnv(name)
	if err != nil {
		return 0, err
	}

	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s should be an integer: %w", name, err)
	}

	if n <= 0 || n > math.MaxInt32 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}

	return int32(n), nil
}

func loadTelemetry() (TelemetryConfig, error) {
	var cfg TelemetryConfig
	var err error
	if cfg.ServiceName, err = requiredEnv("OTEL_SERVICE_NAME"); err != nil {
		return cfg, err
	}

	if cfg.Endpoint, err = requiredEnv("OTEL_EXPORTER_OTLP_ENDPOINT"); err != nil {
		return cfg, err
	}

	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return cfg, fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT must be an HTTP or HTTPS URL")
	}

	if cfg.Protocol, err = requiredEnv("OTEL_EXPORTER_OTLP_PROTOCOL"); err != nil {
		return cfg, err
	}

	if cfg.Protocol != "http/protobuf" && cfg.Protocol != "grpc" {
		return cfg, fmt.Errorf("OTEL_EXPORTER_OTLP_PROTOCOL must be http/protobuf or grpc")
	}

	if cfg.Sampler, err = requiredEnv("OTEL_TRACES_SAMPLER"); err != nil {
		return cfg, err
	}

	cfg.SamplerRatio = 1
	switch cfg.Sampler {
	case "always_on", "always_off", "parentbased_always_on", "parentbased_always_off":
	case "traceidratio", "parentbased_traceidratio":
		if raw := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); raw != "" {
			cfg.SamplerRatio, err = strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(cfg.SamplerRatio) || cfg.SamplerRatio < 0 || cfg.SamplerRatio > 1 {
				return cfg, fmt.Errorf("OTEL_TRACES_SAMPLER_ARG must be a number between 0 and 1")
			}
		}
	default:
		return cfg, fmt.Errorf("unsupported OTEL_TRACES_SAMPLER %q", cfg.Sampler)
	}

	return cfg, nil
}
