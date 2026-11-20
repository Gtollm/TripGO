//go:build integration

package container_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	pgcontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"go.opentelemetry.io/otel/metric/noop"

	domain "github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/infra/postgres/transaction"
	"github.com/Gtollm/TripGO/internal/infra/telemetry"
	"github.com/Gtollm/TripGO/internal/model"
	"github.com/Gtollm/TripGO/internal/repository/idempotency"
	positionrepo "github.com/Gtollm/TripGO/internal/repository/position"
	"github.com/Gtollm/TripGO/internal/repository/trip"
	createposition "github.com/Gtollm/TripGO/internal/usecase/position/create"
	listpositions "github.com/Gtollm/TripGO/internal/usecase/position/list"
	"github.com/Gtollm/TripGO/internal/usecase/trip/create"
	"github.com/Gtollm/TripGO/internal/usecase/trip/finish"
)

const postgresImage = "docker.io/library/postgres"

type recordingCanceller struct {
	mu  sync.Mutex
	ids []uuid.UUID
}

func (c *recordingCanceller) Cancel(_ context.Context, tripID uuid.UUID) {
	c.mu.Lock()
	c.ids = append(c.ids, tripID)
	c.mu.Unlock()
}

func (c *recordingCanceller) Calls() []uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]uuid.UUID, len(c.ids))
	copy(out, c.ids)
	return out
}

type database struct {
	pool      *pgxpool.Pool
	trips     *trip.Repository
	positions *positionrepo.Repository
	keys      *idempotency.Repository
	tx        *transaction.Manager
	canceller *recordingCanceller
	metrics   *telemetry.TripMetrics
	create    *create.UseCase
	finish    *finish.UseCase
	createPos *createposition.UseCase
	listPos   *listpositions.UseCase
}

func newDatabase(t *testing.T, uri string) database {
	t.Helper()

	admin, err := pgx.Connect(t.Context(), uri)
	require.NoError(t, err)

	schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(t.Context(), "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	require.NoError(t, err)

	t.Cleanup(
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
			require.NoError(t, err)
			require.NoError(t, admin.Close(ctx))
		},
	)

	parsed, err := url.Parse(uri)
	require.NoError(t, err)

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	pool, err := pgxpool.New(t.Context(), parsed.String())
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	sqlDB := stdlib.OpenDB(*pool.Config().ConnConfig)
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, os.DirFS("../../migrations"))
	require.NoError(t, err)

	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	db := database{
		pool:      pool,
		trips:     trip.New(pool, 5*time.Second),
		positions: positionrepo.New(pool, 5*time.Second),
		keys:      idempotency.New(pool, 5*time.Second),
		tx:        transaction.New(pool, 5*time.Second),
		canceller: &recordingCanceller{},
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	metrics, err := telemetry.NewTripMetrics(noop.NewMeterProvider().Meter("test"), db.trips, logger)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, metrics.Close()) })

	db.metrics = metrics
	db.create = create.New(db.trips, db.keys, db.tx, metrics, logger, 24*time.Hour, time.Now)
	db.finish = finish.New(db.trips, db.tx, metrics, logger, time.Now)
	db.createPos = createposition.New(db.trips, db.positions, db.tx, db.canceller, metrics, logger, time.Now)
	db.listPos = listpositions.New(db.trips, db.positions)

	return db
}

func createInput() model.CreateTrip {
	return model.CreateTrip{
		UserID:   uuid.New(),
		DriverID: uuid.New(),
		Start:    domain.Coordinates{Latitude: 55.75, Longitude: 37.61},
		End:      domain.Coordinates{Latitude: 55.8, Longitude: 37.7},
		Price:    12500,
	}
}

func TestPostgres(t *testing.T) {
	uri := os.Getenv("TRIPGO_TEST_DATABASE_URL")
	if uri == "" {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		pg, err := pgcontainer.Run(
			ctx, postgresImage, pgcontainer.WithDatabase("tripgo"), pgcontainer.WithUsername("tripgo"),
			pgcontainer.WithPassword("tripgo-test"), pgcontainer.BasicWaitStrategies(),
		)
		require.NoError(t, err)
		testcontainers.CleanupContainer(t, pg)
		uri, err = pg.ConnectionString(t.Context(), "sslmode=disable")
		require.NoError(t, err)
	}

	for name, test := range map[string]func(*testing.T, database){
		"trip lifecycle":                  testLifecycle,
		"rollback second insert":          testCreateRollback,
		"transaction cleanup":             testTransactionCleanup,
		"concurrent create":               testConcurrentCreate,
		"concurrent finish":               testConcurrentFinish,
		"concurrent idempotency":          testConcurrentIdempotency,
		"expired idempotency":             testExpiredIdempotency,
		"list stale trips and batching":   testListStaleTrips,
		"position lifecycle and rollback": testPositionLifecycleAndRollback,
	} {
		t.Run(name, func(t *testing.T) { test(t, newDatabase(t, uri)) })
	}
}

func testLifecycle(t *testing.T, db database) {
	ctx := t.Context()
	input := createInput()
	key := uuid.New()
	input.IdempotencyKey = &key

	created, err := db.create.Run(ctx, input)
	require.NoError(t, err)
	require.False(t, created.Repeated)

	found, err := db.trips.Get(ctx, created.Trip.ID)
	require.NoError(t, err)
	require.Equal(t, input.DriverID, found.DriverID)
	require.Equal(t, domain.StatusACTIVE, found.Status)
	require.Equal(t, input.Price, found.Price)

	active, err := db.trips.CountActive(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, active)

	replayed, err := db.create.Run(ctx, input)
	require.NoError(t, err)
	require.True(t, replayed.Repeated)
	require.Equal(t, found.ID, replayed.Trip.ID)

	input.Price++
	_, err = db.create.Run(ctx, input)
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)

	input.IdempotencyKey = nil
	_, err = db.create.Run(ctx, input)
	require.ErrorIs(t, err, domain.ErrDriverBusy)

	finished, err := db.finish.Run(ctx, model.FinishTrip{ID: found.ID})
	require.NoError(t, err)
	require.Equal(t, domain.StatusCOMPLETED, finished.Trip.Status)
	require.NotNil(t, finished.Trip.FinishedAt)

	_, err = db.finish.Run(ctx, model.FinishTrip{ID: found.ID})
	require.ErrorIs(t, err, domain.ErrTripCompleted)

	_, err = db.trips.Get(ctx, uuid.New())
	require.ErrorIs(t, err, domain.ErrTripNotFound)

	active, err = db.trips.CountActive(ctx)
	require.NoError(t, err)
	require.Zero(t, active)

	var history int
	require.NoError(
		t,
		db.pool.QueryRow(ctx, "SELECT count(*) FROM trip_status_history WHERE trip_id = $1", found.ID).Scan(&history),
	)
	require.Equal(t, 2, history)

	_, err = db.create.Run(ctx, input)
	require.NoError(t, err)
}

func newTrip(t *testing.T) (model.Trip, model.Transition) {
	t.Helper()

	input := createInput()
	created, change, err := domain.New(
		uuid.New(), input.UserID, input.DriverID, input.Start, input.End, input.Price,
		time.Now().UTC().Truncate(time.Microsecond),
	)
	require.NoError(t, err)

	return model.Trip(created), model.Transition(change)
}

func testCreateRollback(t *testing.T, db database) {
	created, change := newTrip(t)
	change.To = domain.StatusNONE

	err := db.tx.Do(t.Context(), func(ctx context.Context) error { return db.trips.Create(ctx, created, change) })
	require.ErrorContains(t, err, "trip_status_history")

	_, err = db.trips.Get(t.Context(), created.ID)
	require.ErrorIs(t, err, domain.ErrTripNotFound)

	var history int
	require.NoError(t, db.pool.QueryRow(t.Context(), "SELECT count(*) FROM trip_status_history").Scan(&history))
	require.Zero(t, history)
}

func testTransactionCleanup(t *testing.T, db database) {
	const cancelScenario = "cancel"
	const panicScenario = "panic"
	for _, scenario := range []string{"error", "nested", panicScenario, cancelScenario} {
		t.Run(
			scenario, func(t *testing.T) {
				created, change := newTrip(t)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				failure := errors.New("abort transaction")
				run := func() error {
					return db.tx.Do(
						ctx, func(ctx context.Context) error {
							if scenario == "nested" {
								if err := db.tx.Do(
									ctx,
									func(ctx context.Context) error { return db.trips.Create(ctx, created, change) },
								); err != nil {
									return err
								}
							} else if err := db.trips.Create(ctx, created, change); err != nil {
								return err
							}

							if scenario == panicScenario {
								panic("abort transaction")
							}

							if scenario == cancelScenario {
								cancel()
								return nil
							}

							return failure
						},
					)
				}

				switch scenario {
				case panicScenario:
					require.PanicsWithValue(t, "abort transaction", func() { _ = run() })
				case cancelScenario:
					require.ErrorIs(t, run(), context.Canceled)
				default:
					require.ErrorIs(t, run(), failure)
				}

				_, err := db.trips.Get(t.Context(), created.ID)
				require.ErrorIs(t, err, domain.ErrTripNotFound)
			},
		)
	}
}

func testConcurrentCreate(t *testing.T, db database) {
	input := createInput()
	start, results := make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := db.create.Run(t.Context(), input)
			results <- err
		}()
	}

	close(start)
	assertOneConflict(t, results, domain.ErrDriverBusy)

	active, err := db.trips.CountActive(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, active)
}

func testConcurrentFinish(t *testing.T, db database) {
	created, err := db.create.Run(t.Context(), createInput())
	require.NoError(t, err)

	start, results := make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := db.finish.Run(t.Context(), model.FinishTrip{ID: created.Trip.ID})
			results <- err
		}()
	}

	close(start)
	assertOneConflict(t, results, domain.ErrTripCompleted)

	var history int
	require.NoError(t, db.pool.QueryRow(t.Context(), "SELECT count(*) FROM trip_status_history").Scan(&history))
	require.Equal(t, 2, history)
}

func assertOneConflict(t *testing.T, results <-chan error, conflict error) {
	t.Helper()

	successes, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, conflict)
			conflicts++
		}
	}

	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
}

func testConcurrentIdempotency(t *testing.T, db database) {
	input := createInput()
	key := uuid.New()
	input.IdempotencyKey = &key
	type result struct {
		trip model.CreatedTrip
		err  error
	}

	start, results := make(chan struct{}), make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			created, err := db.create.Run(t.Context(), input)
			results <- result{created, err}
		}()
	}

	close(start)
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, first.trip.Trip.ID, second.trip.Trip.ID)
	require.NotEqual(t, first.trip.Repeated, second.trip.Repeated)

	var history int
	require.NoError(t, db.pool.QueryRow(t.Context(), "SELECT count(*) FROM trip_status_history").Scan(&history))
	require.Equal(t, 1, history)
}

func testExpiredIdempotency(t *testing.T, db database) {
	input := createInput()
	key := uuid.New()
	input.IdempotencyKey = &key

	first, err := db.create.Run(t.Context(), input)
	require.NoError(t, err)

	_, err = db.pool.Exec(
		t.Context(), "UPDATE idempotency_keys SET created_at = now() - interval '25 hours' WHERE idempotency_key = $1",
		key,
	)
	require.NoError(t, err)

	input.DriverID = uuid.New()
	second, err := db.create.Run(t.Context(), input)
	require.NoError(t, err)
	require.False(t, second.Repeated)
	require.NotEqual(t, first.Trip.ID, second.Trip.ID)

	stored, err := db.keys.Get(t.Context(), key)
	require.NoError(t, err)
	require.Equal(t, second.Trip.ID, stored.TripID)
}

type failingLastPositionTrips struct {
	*trip.Repository
	err error
}

func (f failingLastPositionTrips) UpdateLastPositionAt(context.Context, uuid.UUID, time.Time, time.Time) error {
	return f.err
}

func testPositionLifecycleAndRollback(t *testing.T, db database) {
	ctx := t.Context()
	created, err := db.create.Run(ctx, createInput())
	require.NoError(t, err)
	tripID := created.Trip.ID

	base := time.Now().UTC().Truncate(time.Microsecond)
	t1 := base.Add(-20 * time.Second)
	t2 := base.Add(-5 * time.Second)

	p2, err := db.createPos.Run(ctx, model.CreatePosition{
		TripID:      tripID,
		Coordinates: domain.Coordinates{Latitude: 59.935, Longitude: 30.335},
		RecordedAt:  t2,
	})
	require.NoError(t, err)

	storedTrip, err := db.trips.Get(ctx, tripID)
	require.NoError(t, err)
	require.NotNil(t, storedTrip.LastPositionAt)
	require.True(t, t2.Equal(*storedTrip.LastPositionAt))
	require.Equal(t, []uuid.UUID{tripID}, db.canceller.Calls())

	p1, err := db.createPos.Run(ctx, model.CreatePosition{
		TripID:      tripID,
		Coordinates: domain.Coordinates{Latitude: 59.931, Longitude: 30.331},
		RecordedAt:  t1,
	})
	require.NoError(t, err)

	storedTrip, err = db.trips.Get(ctx, tripID)
	require.NoError(t, err)
	require.NotNil(t, storedTrip.LastPositionAt)
	require.True(t, t2.Equal(*storedTrip.LastPositionAt))

	p1Tie, err := db.createPos.Run(ctx, model.CreatePosition{
		TripID:      tripID,
		Coordinates: domain.Coordinates{Latitude: 59.932, Longitude: 30.332},
		RecordedAt:  t1,
	})
	require.NoError(t, err)

	route, err := db.listPos.Run(ctx, model.ListPositions{TripID: tripID})
	require.NoError(t, err)
	require.Len(t, route.Positions, 3)
	require.Equal(t, []int64{p1.Position.ID, p1Tie.Position.ID, p2.Position.ID}, []int64{
		route.Positions[0].ID, route.Positions[1].ID, route.Positions[2].ID,
	})

	rollbackErr := errors.New("forced update last_position_at failure")
	failingUseCase := createposition.New(
		failingLastPositionTrips{Repository: db.trips, err: rollbackErr},
		db.positions,
		db.tx,
		db.canceller,
		db.metrics,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		time.Now,
	)
	_, err = failingUseCase.Run(ctx, model.CreatePosition{
		TripID:      tripID,
		Coordinates: domain.Coordinates{Latitude: 59.94, Longitude: 30.34},
		RecordedAt:  base,
	})
	require.ErrorIs(t, err, rollbackErr)

	routeAfterRollback, err := db.listPos.Run(ctx, model.ListPositions{TripID: tripID})
	require.NoError(t, err)
	require.Len(t, routeAfterRollback.Positions, 3)

	storedTrip, err = db.trips.Get(ctx, tripID)
	require.NoError(t, err)
	require.NotNil(t, storedTrip.LastPositionAt)
	require.True(t, t2.Equal(*storedTrip.LastPositionAt))

	_, err = db.finish.Run(ctx, model.FinishTrip{ID: tripID})
	require.NoError(t, err)

	_, err = db.createPos.Run(ctx, model.CreatePosition{
		TripID:      tripID,
		Coordinates: domain.Coordinates{Latitude: 59.95, Longitude: 30.35},
		RecordedAt:  base,
	})
	require.ErrorIs(t, err, domain.ErrTripCompleted)

	routeAfterFinish, err := db.listPos.Run(ctx, model.ListPositions{TripID: tripID})
	require.NoError(t, err)
	require.Len(t, routeAfterFinish.Positions, 3)
}

func testListStaleTrips(t *testing.T, db database) {
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)

	insertTrip := func(id uuid.UUID, status string, startedAt time.Time, finishedAt, lastPosAt *time.Time) {
		_, err := db.pool.Exec(ctx, `
INSERT INTO trips (
	id, user_id, driver_id,
	start_latitude, start_longitude, end_latitude, end_longitude,
	price, status, started_at, finished_at, last_position_at
) VALUES ($1, $2, $3, 59.9, 30.3, 59.95, 30.35, 1000, $4, $5, $6, $7)`,
			id, uuid.New(), uuid.New(), status, startedAt, finishedAt, lastPosAt,
		)
		require.NoError(t, err)
	}

	stalePosTime := now.Add(-10 * time.Minute)
	staleNoPosID := uuid.New()
	stalePosID := uuid.New()
	freshNoPosID := uuid.New()
	freshPosID := uuid.New()
	completedID := uuid.New()

	insertTrip(stalePosID, "active", now.Add(-20*time.Minute), nil, &stalePosTime)
	insertTrip(staleNoPosID, "active", now.Add(-5*time.Minute), nil, nil)
	insertTrip(freshNoPosID, "active", now.Add(-10*time.Second), nil, nil)
	freshPosTime := now.Add(-10 * time.Second)
	insertTrip(freshPosID, "active", now.Add(-20*time.Minute), nil, &freshPosTime)
	finishedAt := now.Add(-15 * time.Minute)
	completedPosTime := now.Add(-25 * time.Minute)
	insertTrip(completedID, "completed", now.Add(-30*time.Minute), &finishedAt, &completedPosTime)

	tieTime := now.Add(-3 * time.Minute)
	tieIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	sort.Slice(tieIDs, func(i, j int) bool { return tieIDs[i].String() < tieIDs[j].String() })
	for _, id := range tieIDs {
		insertTrip(id, "active", now.Add(-10*time.Minute), nil, &tieTime)
	}

	var got []uuid.UUID
	var cursor *model.StaleCursor
	batches := 0
	for {
		batch, err := db.trips.ListStaleBatch(ctx, time.Minute, cursor, 2)
		require.NoError(t, err)
		batches++
		for _, item := range batch {
			got = append(got, item.TripID)
		}
		if len(batch) < 2 {
			break
		}
		last := batch[len(batch)-1]
		cursor = &model.StaleCursor{LastActiveAt: last.LastActiveAt, TripID: last.TripID}
	}

	want := append([]uuid.UUID{stalePosID, staleNoPosID}, tieIDs...)
	require.Equal(t, want, got)
	require.Equal(t, 3, batches)
}
