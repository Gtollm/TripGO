package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gtollm/TripGO/internal/config"
	"github.com/Gtollm/TripGO/internal/handler"
	"github.com/Gtollm/TripGO/internal/infrastructure/postgres"
	"github.com/Gtollm/TripGO/internal/repository"
	"github.com/Gtollm/TripGO/internal/service"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("trip-service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, err := newLogger(cfg.LogLevel)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := newPool(ctx, cfg.DatabaseConfig)
	if err != nil {
		return err
	}

	server := newHTTPServer(cfg, pool, logger)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HttpConfig.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		pool.Close()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown budget exceeded: %w", err)
	}

	pool.Close()
	logger.Info("shutdown complete")
	return nil
}

func newLogger(rawLevel string) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(rawLevel)); err != nil {
		return nil, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})), nil
}

func newPool(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConnections
	poolCfg.MinConns = cfg.MinConnections
	poolCfg.MaxConnLifetime = cfg.MaxConnectionLifetime
	poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

func newHTTPServer(cfg *config.Config, pool *pgxpool.Pool, logger *slog.Logger) *http.Server {
	repo := repository.NewPostgresRepository(pool, cfg.DatabaseConfig.QueryTimeout)
	keys := repository.NewPostgresIdempotencyRepository(pool, cfg.DatabaseConfig.QueryTimeout)
	trips := service.New(repo, keys, postgres.NewTxManager(pool), cfg.IdempotencyKeyTTL)
	httpHandler := handler.New(trips, pool, logger, cfg.DatabaseConfig.QueryTimeout)

	return &http.Server{
		Addr:              cfg.HttpConfig.Addr,
		Handler:           httpHandler.Routes(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}
