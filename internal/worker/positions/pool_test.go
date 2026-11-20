package positions_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/Gtollm/TripGO/internal/config"
	"github.com/Gtollm/TripGO/internal/infra/telemetry"
	"github.com/Gtollm/TripGO/internal/model"
	"github.com/Gtollm/TripGO/internal/worker/positions"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

type fakeTicker struct {
	ch      chan time.Time
	stopped atomic.Bool
}

func (t *fakeTicker) C() <-chan time.Time { return t.ch }
func (t *fakeTicker) Stop()               { t.stopped.Store(true) }
func (t *fakeTicker) Tick(ctx context.Context) {
	select {
	case t.ch <- time.Now():
	case <-ctx.Done():
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fixture struct {
	pool     *positions.Pool
	repo     *MockstaleTripsRepository
	push     *MockpushClient
	ticker   *fakeTicker
	reader   *sdkmetric.ManualReader
	exporter *tracetest.InMemoryExporter
	logs     *syncBuffer
	base     time.Time
}

func setup(t *testing.T, workers, queueSize, batchSize int) fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	repo, push := NewMockstaleTripsRepository(ctrl), NewMockpushClient(ctrl)
	ticker := &fakeTicker{ch: make(chan time.Time)}

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, mp.Shutdown(context.Background())) })

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { require.NoError(t, tp.Shutdown(context.Background())) })

	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

	metrics, err := telemetry.NewPositionMetrics(mp.Meter("test"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, metrics.Close()) })

	cfg := config.PositionWorkerConfig{
		CheckInterval: time.Minute, StaleAfter: time.Minute,
		Workers: workers, QueueSize: queueSize, ScanBatchSize: batchSize,
	}
	pool := positions.New(repo, push, metrics, tp, logger, cfg, time.Second, func(time.Duration) positions.Ticker { return ticker })

	return fixture{
		pool: pool, repo: repo, push: push, ticker: ticker,
		reader: reader, exporter: exporter, logs: logs,
		base: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}
}

func (f fixture) expectBatch(cursor *model.StaleCursor, limit int, trips ...model.StaleTrip) *gomock.Call {
	return f.repo.EXPECT().ListStaleBatch(gomock.Any(), time.Minute, cursor, limit).Return(trips, nil)
}

func (f fixture) expectEmptyTick() <-chan struct{} {
	done := make(chan struct{})
	f.repo.EXPECT().ListStaleBatch(gomock.Any(), time.Minute, nil, gomock.Any()).DoAndReturn(
		func(context.Context, time.Duration, *model.StaleCursor, int) ([]model.StaleTrip, error) {
			close(done)
			return nil, nil
		},
	)
	return done
}

func (f fixture) blockPush(trip model.StaleTrip) (started <-chan struct{}, release chan struct{}) {
	s, r := make(chan struct{}), make(chan struct{})
	f.push.EXPECT().RequestPosition(gomock.Any(), trip.TripID, trip.DriverID).DoAndReturn(
		func(context.Context, uuid.UUID, uuid.UUID) error { close(s); <-r; return nil },
	)
	return s, r
}

func (f fixture) expectPush(trips ...model.StaleTrip) {
	for _, tr := range trips {
		f.push.EXPECT().RequestPosition(gomock.Any(), tr.TripID, tr.DriverID).Return(nil)
	}
}

func (f fixture) tick(t *testing.T, n int) {
	t.Helper()
	for range n {
		f.ticker.Tick(t.Context())
	}
}

func (f fixture) counter(t *testing.T, name, resultVal string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, f.reader.Collect(t.Context(), &rm))
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == name {
				for _, dp := range sum.DataPoints {
					if val, has := dp.Attributes.Value(attribute.Key("result")); resultVal == "" || (has && val.AsString() == resultVal) {
						total += dp.Value
					}
				}
			}
		}
	}
	return total
}

func (f fixture) histSum(t *testing.T, name string) (count uint64, sum int64) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, f.reader.Collect(t.Context(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if hist, ok := m.Data.(metricdata.Histogram[int64]); ok && m.Name == name {
				for _, dp := range hist.DataPoints {
					count += dp.Count
					sum += dp.Sum
				}
			}
		}
	}
	return count, sum
}

func staleTrip(at time.Time) model.StaleTrip {
	return model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New(), LastActiveAt: at}
}

func cursorFor(tr model.StaleTrip) *model.StaleCursor {
	return &model.StaleCursor{LastActiveAt: tr.LastActiveAt, TripID: tr.TripID}
}

func TestPoolDeduplicationAcrossTicks(t *testing.T) {
	t.Parallel()

	f := setup(t, 1, 10, 100)
	t1, t2 := staleTrip(f.base.Add(-5*time.Minute)), staleTrip(f.base.Add(-4*time.Minute))

	gomock.InOrder(f.expectBatch(nil, 100, t1, t2), f.expectBatch(nil, 100, t1, t2))
	tick3Done := f.expectEmptyTick()
	started, release := f.blockPush(t1)
	f.expectPush(t2)

	f.pool.Start(t.Context())
	f.tick(t, 1)
	<-started
	f.tick(t, 2)
	<-tick3Done
	close(release)
	require.NoError(t, f.pool.Shutdown(t.Context()))

	require.EqualValues(t, 2, f.counter(t, "tripgo.stale_checks", "queued"))
	require.EqualValues(t, 2, f.counter(t, "tripgo.stale_checks", "duplicate"))
	require.EqualValues(t, 2, f.counter(t, "tripgo.position_requests", "success"))
}

func TestPoolCancelBeforeWorkerProcessesTask(t *testing.T) {
	t.Parallel()

	f := setup(t, 1, 10, 100)
	blocker, cancelled := staleTrip(f.base.Add(-5*time.Minute)), staleTrip(f.base.Add(-4*time.Minute))

	f.expectBatch(nil, 100, blocker, cancelled)
	scanDone := f.expectEmptyTick()
	started, release := f.blockPush(blocker)

	f.pool.Start(t.Context())
	f.tick(t, 1)
	<-started
	f.tick(t, 1)
	<-scanDone

	f.pool.Cancel(t.Context(), cancelled.TripID)
	close(release)
	require.NoError(t, f.pool.Shutdown(t.Context()))

	require.EqualValues(t, 1, f.counter(t, "tripgo.position_requests", "cancelled"))
	require.EqualValues(t, 1, f.counter(t, "tripgo.position_requests", "success"))
}

func TestPoolOverflowDropsWithoutBlockingScanner(t *testing.T) {
	t.Parallel()

	f := setup(t, 1, 2, 100)
	running := staleTrip(f.base.Add(-10 * time.Minute))
	batch := []model.StaleTrip{
		staleTrip(f.base.Add(-5 * time.Minute)), staleTrip(f.base.Add(-4 * time.Minute)),
		staleTrip(f.base.Add(-3 * time.Minute)), staleTrip(f.base.Add(-2 * time.Minute)),
	}

	gomock.InOrder(f.expectBatch(nil, 100, running), f.expectBatch(nil, 100, batch...))
	scan3Done := f.expectEmptyTick()
	started, release := f.blockPush(running)
	f.expectPush(batch[0], batch[1])

	f.pool.Start(t.Context())
	f.tick(t, 1)
	<-started
	f.tick(t, 2)
	<-scan3Done
	close(release)
	require.NoError(t, f.pool.Shutdown(t.Context()))

	require.EqualValues(t, 1, f.counter(t, "tripgo.stale_checks", "dropped"))
	require.Contains(t, f.logs.String(), `"level":"WARN"`)
}

func TestPoolProcessesNTasksWithConfiguredWorkers(t *testing.T) {
	t.Parallel()

	const numWorkers, numTasks = 4, 20
	f := setup(t, numWorkers, 50, 100)

	trips := make([]model.StaleTrip, numTasks)
	for i := range numTasks {
		trips[i] = staleTrip(f.base.Add(-time.Duration(numTasks-i) * time.Minute))
	}
	f.expectBatch(nil, 100, trips...)

	var (
		curActive, maxActive atomic.Int32
		fourActive, release  = make(chan struct{}), make(chan struct{})
		once                 sync.Once
	)
	f.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).Times(numTasks).DoAndReturn(
		func(context.Context, uuid.UUID, uuid.UUID) error {
			cur := curActive.Add(1)
			for prev := maxActive.Load(); cur > prev && !maxActive.CompareAndSwap(prev, cur); prev = maxActive.Load() {
			}
			if cur == numWorkers {
				once.Do(func() { close(fourActive) })
			}
			<-release
			curActive.Add(-1)
			return nil
		},
	)

	f.pool.Start(t.Context())
	f.tick(t, 1)
	<-fourActive
	close(release)
	require.NoError(t, f.pool.Shutdown(t.Context()))

	require.EqualValues(t, numWorkers, maxActive.Load())
	require.EqualValues(t, numTasks, f.counter(t, "tripgo.position_requests", "success"))
}

func TestPoolErrorResilienceAndSpanLinks(t *testing.T) {
	t.Parallel()

	f := setup(t, 1, 10, 100)
	failTrip, okTrip := staleTrip(f.base.Add(-10*time.Minute)), staleTrip(f.base.Add(-5*time.Minute))

	f.expectBatch(nil, 100, failTrip, okTrip)
	scanDone := f.expectEmptyTick()
	f.push.EXPECT().RequestPosition(gomock.Any(), failTrip.TripID, failTrip.DriverID).Return(errors.New("push 503"))
	f.expectPush(okTrip)

	f.pool.Start(t.Context())
	f.tick(t, 2)
	<-scanDone
	require.NoError(t, f.pool.Shutdown(t.Context()))

	require.EqualValues(t, 1, f.counter(t, "tripgo.position_requests", "error"))
	require.EqualValues(t, 1, f.counter(t, "tripgo.position_requests", "success"))

	var scanSpanCtx trace.SpanContext
	for _, s := range f.exporter.GetSpans() {
		if s.Name == "positions.scan" && !scanSpanCtx.IsValid() {
			scanSpanCtx = s.SpanContext
		}
	}
	require.True(t, scanSpanCtx.IsValid())

	workerSpans := 0
	for _, s := range f.exporter.GetSpans() {
		if s.Name == "positions.request_position" {
			workerSpans++
			require.False(t, s.Parent.IsValid())
			require.Len(t, s.Links, 1)
			require.Equal(t, scanSpanCtx.SpanID(), s.Links[0].SpanContext.SpanID())
		}
	}
	require.Equal(t, 2, workerSpans)
}

func TestPoolShutdownDrainAndTimeout(t *testing.T) {
	t.Parallel()

	t.Run("drains queued tasks", func(t *testing.T) {
		t.Parallel()

		f := setup(t, 1, 10, 100)
		t1, t2, t3 := staleTrip(f.base.Add(-3*time.Minute)), staleTrip(f.base.Add(-2*time.Minute)), staleTrip(f.base.Add(-1*time.Minute))

		f.expectBatch(nil, 100, t1, t2, t3)
		started, release := f.blockPush(t1)
		f.expectPush(t2, t3)

		f.pool.Start(t.Context())
		f.tick(t, 1)
		<-started

		shutdownDone := make(chan error, 1)
		go func() { shutdownDone <- f.pool.Shutdown(t.Context()) }()
		close(release)

		require.NoError(t, <-shutdownDone)
		require.EqualValues(t, 3, f.counter(t, "tripgo.position_requests", "success"))
		require.True(t, f.ticker.stopped.Load())
	})

	t.Run("forced timeout exits cleanly", func(t *testing.T) {
		t.Parallel()

		f := setup(t, 1, 10, 100)
		stuck := staleTrip(f.base.Add(-3 * time.Minute))
		f.expectBatch(nil, 100, stuck)

		started := make(chan struct{})
		f.push.EXPECT().RequestPosition(gomock.Any(), stuck.TripID, stuck.DriverID).DoAndReturn(
			func(ctx context.Context, _, _ uuid.UUID) error { close(started); <-ctx.Done(); return ctx.Err() },
		)

		f.pool.Start(t.Context())
		f.tick(t, 1)
		<-started

		shutdownCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		require.ErrorIs(t, f.pool.Shutdown(shutdownCtx), context.DeadlineExceeded)
	})
}

func TestPoolBatchScanning(t *testing.T) {
	t.Parallel()

	t.Run("250 trips batch 100 queue 300 executes 3 queries", func(t *testing.T) {
		t.Parallel()

		f := setup(t, 4, 300, 100)
		allTrips := make([]model.StaleTrip, 250)
		for i := range 250 {
			allTrips[i] = model.StaleTrip{
				TripID:       uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1)),
				DriverID:     uuid.New(),
				LastActiveAt: f.base.Add(-time.Duration(300-i) * time.Second),
			}
		}

		gomock.InOrder(
			f.expectBatch(nil, 100, allTrips[0:100]...),
			f.expectBatch(cursorFor(allTrips[99]), 100, allTrips[100:200]...),
			f.expectBatch(cursorFor(allTrips[199]), 100, allTrips[200:250]...),
		)
		scanDone := f.expectEmptyTick()
		f.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).Times(250).Return(nil)

		f.pool.Start(t.Context())
		f.tick(t, 2)
		<-scanDone
		require.NoError(t, f.pool.Shutdown(t.Context()))

		require.EqualValues(t, 250, f.counter(t, "tripgo.stale_checks", "queued"))
		histCount, histSum := f.histSum(t, "tripgo.stale_checks.rows_read")
		require.EqualValues(t, 2, histCount)
		require.EqualValues(t, 250, histSum)
	})

	t.Run("250 trips batch 100 queue 50 stops after first batch", func(t *testing.T) {
		t.Parallel()

		f := setup(t, 1, 50, 100)
		blocker := staleTrip(f.base.Add(-10 * time.Hour))
		firstBatch := make([]model.StaleTrip, 100)
		for i := range 100 {
			firstBatch[i] = staleTrip(f.base.Add(-time.Duration(200-i) * time.Second))
		}

		gomock.InOrder(f.expectBatch(nil, 100, blocker), f.expectBatch(nil, 100, firstBatch...))
		tick3Done := f.expectEmptyTick()
		started, release := f.blockPush(blocker)
		f.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).Times(50).Return(nil)

		f.pool.Start(t.Context())
		f.tick(t, 1)
		<-started
		f.tick(t, 2)
		<-tick3Done
		close(release)
		require.NoError(t, f.pool.Shutdown(t.Context()))

		require.EqualValues(t, 51, f.counter(t, "tripgo.stale_checks", "queued"))
		require.EqualValues(t, 1, f.counter(t, "tripgo.stale_checks", "dropped"))
		histCount, histSum := f.histSum(t, "tripgo.stale_checks.rows_read")
		require.EqualValues(t, 3, histCount)
		require.EqualValues(t, 101, histSum)
	})

	t.Run("equal timestamps across batch boundary lose none", func(t *testing.T) {
		t.Parallel()

		f := setup(t, 1, 10, 2)
		sameTime := f.base.Add(-time.Hour)
		trips := []model.StaleTrip{
			{TripID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), DriverID: uuid.New(), LastActiveAt: sameTime},
			{TripID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), DriverID: uuid.New(), LastActiveAt: sameTime},
			{TripID: uuid.MustParse("00000000-0000-0000-0000-000000000003"), DriverID: uuid.New(), LastActiveAt: sameTime},
		}

		gomock.InOrder(f.expectBatch(nil, 2, trips[0:2]...), f.expectBatch(cursorFor(trips[1]), 2, trips[2]))
		scanDone := f.expectEmptyTick()
		f.expectPush(trips...)

		f.pool.Start(t.Context())
		f.tick(t, 2)
		<-scanDone
		require.NoError(t, f.pool.Shutdown(t.Context()))
	})

	t.Run("eviction stops reading further batches", func(t *testing.T) {
		t.Parallel()

		f := setup(t, 1, 1, 1)
		blocker, fresh, veryOld := staleTrip(f.base.Add(-10*time.Hour)), staleTrip(f.base.Add(-time.Minute)), staleTrip(f.base.Add(-30*time.Minute))

		gomock.InOrder(
			f.expectBatch(nil, 1, blocker),
			f.expectBatch(cursorFor(blocker), 1),
			f.expectBatch(nil, 1, fresh),
			f.expectBatch(cursorFor(fresh), 1),
			f.expectBatch(nil, 1, veryOld),
		)
		tick4Done := f.expectEmptyTick()
		started, release := f.blockPush(blocker)
		f.expectPush(veryOld)

		f.pool.Start(t.Context())
		f.tick(t, 1)
		<-started
		f.tick(t, 3)
		<-tick4Done
		close(release)
		require.NoError(t, f.pool.Shutdown(t.Context()))

		require.EqualValues(t, 1, f.counter(t, "tripgo.stale_checks", "dropped"))
		require.EqualValues(t, 2, f.counter(t, "tripgo.position_requests", "success"))
	})
}

func TestPoolPriorityOrderingAndEvictionAcrossTicks(t *testing.T) {
	t.Parallel()

	f := setup(t, 1, 2, 100)
	blocker := staleTrip(f.base.Add(-10 * time.Hour))
	mid, fresh, veryOld := staleTrip(f.base.Add(-5*time.Minute)), staleTrip(f.base.Add(-time.Minute)), staleTrip(f.base.Add(-30*time.Minute))

	gomock.InOrder(
		f.expectBatch(nil, 100, blocker),
		f.expectBatch(nil, 100, fresh, mid),
		f.expectBatch(nil, 100, veryOld),
	)
	tick4Done := f.expectEmptyTick()
	started, release := f.blockPush(blocker)
	gomock.InOrder(
		f.push.EXPECT().RequestPosition(gomock.Any(), veryOld.TripID, veryOld.DriverID).Return(nil),
		f.push.EXPECT().RequestPosition(gomock.Any(), mid.TripID, mid.DriverID).Return(nil),
	)

	f.pool.Start(t.Context())
	f.tick(t, 1)
	<-started
	f.tick(t, 3)
	<-tick4Done
	close(release)
	require.NoError(t, f.pool.Shutdown(t.Context()))

	require.EqualValues(t, 1, f.counter(t, "tripgo.stale_checks", "dropped"))
	require.Contains(t, f.logs.String(), fresh.TripID.String())
}

func TestPoolCancelDuringScanPreventsSpuriousPush(t *testing.T) {
	t.Parallel()

	f := setup(t, 1, 10, 100)
	blocker, alreadyQueued, midScan := staleTrip(f.base.Add(-10*time.Minute)), staleTrip(f.base.Add(-6*time.Minute)), staleTrip(f.base.Add(-5*time.Minute))

	scan2Started, releaseScan2 := make(chan struct{}), make(chan struct{})
	gomock.InOrder(
		f.expectBatch(nil, 100, blocker, alreadyQueued),
		f.repo.EXPECT().ListStaleBatch(gomock.Any(), time.Minute, nil, 100).DoAndReturn(
			func(context.Context, time.Duration, *model.StaleCursor, int) ([]model.StaleTrip, error) {
				close(scan2Started)
				<-releaseScan2
				return []model.StaleTrip{alreadyQueued, midScan}, nil
			},
		),
	)
	tick3Done := f.expectEmptyTick()
	blockerStarted, blockerRelease := f.blockPush(blocker)

	f.pool.Start(t.Context())
	f.tick(t, 1)
	<-blockerStarted
	f.tick(t, 1)
	<-scan2Started

	f.pool.Cancel(t.Context(), alreadyQueued.TripID)
	f.pool.Cancel(t.Context(), midScan.TripID)
	close(releaseScan2)

	f.tick(t, 1)
	<-tick3Done
	close(blockerRelease)
	require.NoError(t, f.pool.Shutdown(t.Context()))

	require.EqualValues(t, 2, f.counter(t, "tripgo.stale_checks", "queued"))
	require.EqualValues(t, 0, f.counter(t, "tripgo.stale_checks", "duplicate"))
	require.EqualValues(t, 1, f.counter(t, "tripgo.position_requests", "cancelled"))
	require.EqualValues(t, 1, f.counter(t, "tripgo.position_requests", "success"))
}
