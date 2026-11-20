package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/propagation"

	"github.com/Gtollm/TripGO/internal/config"
	"github.com/Gtollm/TripGO/internal/gateway/push"
	transport "github.com/Gtollm/TripGO/internal/http"
	"github.com/Gtollm/TripGO/internal/http/middleware"
	createpositionhttp "github.com/Gtollm/TripGO/internal/http/position/create"
	listpositionshttp "github.com/Gtollm/TripGO/internal/http/position/list"
	"github.com/Gtollm/TripGO/internal/http/ready"
	createhttp "github.com/Gtollm/TripGO/internal/http/trip/create"
	finishhttp "github.com/Gtollm/TripGO/internal/http/trip/finish"
	gethttp "github.com/Gtollm/TripGO/internal/http/trip/get"
	"github.com/Gtollm/TripGO/internal/infra/postgres"
	"github.com/Gtollm/TripGO/internal/infra/postgres/transaction"
	"github.com/Gtollm/TripGO/internal/infra/telemetry"
	"github.com/Gtollm/TripGO/internal/repository/idempotency"
	positionrepo "github.com/Gtollm/TripGO/internal/repository/position"
	"github.com/Gtollm/TripGO/internal/repository/trip"
	createposition "github.com/Gtollm/TripGO/internal/usecase/position/create"
	listpositions "github.com/Gtollm/TripGO/internal/usecase/position/list"
	"github.com/Gtollm/TripGO/internal/usecase/trip/create"
	"github.com/Gtollm/TripGO/internal/usecase/trip/finish"
	"github.com/Gtollm/TripGO/internal/usecase/trip/get"
	"github.com/Gtollm/TripGO/internal/worker/positions"
)

type Container struct {
	Server          *http.Server
	Logger          *slog.Logger
	PositionWorker  *positions.Pool
	pool            *pgxpool.Pool
	pushGateway     *push.Client
	telemetry       *telemetry.Telemetry
	tripMetrics     *telemetry.TripMetrics
	positionMetrics *telemetry.PositionMetrics
}

func New(ctx context.Context, cfg *config.Config) (_ *Container, err error) {
	c := &Container{}
	defer func() {
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
			defer cancel()
			err = errors.Join(err, c.Shutdown(cleanupCtx))
		}
	}()

	c.telemetry, err = telemetry.New(ctx, cfg.Telemetry, cfg.LogLevel, os.Stdout)
	if err != nil {
		return nil, err
	}

	c.Logger = c.telemetry.Logger
	meter := c.telemetry.Metrics.Meter("trip-service")

	tracer, err := postgres.NewTracer(c.telemetry.Traces, meter)
	if err != nil {
		return nil, err
	}

	c.pool, err = postgres.Open(ctx, cfg.DatabaseConfig, tracer)
	if err != nil {
		return nil, err
	}

	trips := trip.New(c.pool, cfg.DatabaseConfig.QueryTimeout)
	positionsRepo := positionrepo.New(c.pool, cfg.DatabaseConfig.QueryTimeout)
	keys := idempotency.New(c.pool, cfg.DatabaseConfig.QueryTimeout)
	tx := transaction.New(c.pool, cfg.DatabaseConfig.QueryTimeout)

	c.tripMetrics, err = telemetry.NewTripMetrics(meter, trips, c.Logger)
	if err != nil {
		return nil, err
	}

	c.positionMetrics, err = telemetry.NewPositionMetrics(meter)
	if err != nil {
		return nil, err
	}

	propagator := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	c.pushGateway = push.New(cfg.Push, c.telemetry.Traces, propagator)

	c.PositionWorker = positions.New(
		trips,
		c.pushGateway,
		c.positionMetrics,
		c.telemetry.Traces,
		c.Logger,
		cfg.PositionWorker,
		cfg.Push.RequestTimeout,
		positions.NewTicker,
	)

	createTrip := create.New(trips, keys, tx, c.tripMetrics, c.Logger, cfg.IdempotencyKeyTTL, time.Now)
	finishTrip := finish.New(trips, tx, c.tripMetrics, c.Logger, time.Now)
	getTrip := get.New(trips)
	createPos := createposition.New(trips, positionsRepo, tx, c.PositionWorker, c.tripMetrics, c.Logger, time.Now)
	listPos := listpositions.New(trips, positionsRepo)

	router := transport.NewRouter(
		createhttp.New(createTrip),
		gethttp.New(getTrip),
		finishhttp.New(finishTrip),
		createpositionhttp.New(createPos),
		listpositionshttp.New(listPos),
		ready.New(c.pool, cfg.DatabaseConfig.QueryTimeout),
	)

	metrics, err := middleware.NewMetrics(meter)
	if err != nil {
		return nil, err
	}

	handler := middleware.Recover(c.Logger)(middleware.Tracing(c.telemetry.Traces, propagator, router)(
		middleware.Logging(c.Logger)(metrics.Wrap(router)),
	))

	c.Server = &http.Server{
		Addr:              cfg.HttpConfig.Addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.HttpConfig.ReadHeaderTimeout,
		ReadTimeout:       cfg.HttpConfig.ReadTimeout,
		WriteTimeout:      cfg.HttpConfig.WriteTimeout,
		IdleTimeout:       cfg.HttpConfig.IdleTimeout,
	}

	return c, nil
}

func (c *Container) Shutdown(ctx context.Context) error {
	var result error

	if c.Server != nil {
		if err := c.Server.Shutdown(ctx); err != nil {
			result = errors.Join(result, fmt.Errorf("shutdown http: %w", err), c.Server.Close())
		}
	}

	if c.PositionWorker != nil {
		result = errors.Join(result, c.PositionWorker.Shutdown(ctx))
	}

	if c.pushGateway != nil {
		c.pushGateway.Close()
	}

	if c.positionMetrics != nil {
		result = errors.Join(result, c.positionMetrics.Close())
	}

	if c.tripMetrics != nil {
		result = errors.Join(result, c.tripMetrics.Close())
	}

	if c.pool != nil {
		done := make(chan struct{})
		go func() {
			c.pool.Close()
			close(done)
		}()

		select {
		case <-done:
		case <-ctx.Done():
			result = errors.Join(result, fmt.Errorf("close postgres pool: %w", ctx.Err()))
		}
	}

	if c.Logger != nil {
		c.Logger.InfoContext(ctx, "service stopped")
	}

	if c.telemetry != nil {
		result = errors.Join(result, c.telemetry.Shutdown(ctx))
	}

	return result
}
