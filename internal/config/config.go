package config

import (
	"fmt"
	"math"
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
	Addr string
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := loadFromEnv(cfg); err != nil {
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
