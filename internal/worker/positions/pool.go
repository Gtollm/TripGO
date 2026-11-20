package positions

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/Gtollm/TripGO/internal/config"
	"github.com/Gtollm/TripGO/internal/model"
)

const (
	tracerName           = "github.com/Gtollm/TripGO/internal/worker/positions"
	staleResultQueued    = "queued"
	staleResultDuplicate = "duplicate"
	staleResultDropped   = "dropped"
	requestResultSuccess = "success"
	requestResultError   = "error"
)

type Pool struct {
	repo          staleTripsRepository
	push          pushClient
	queue         *PriorityQueue
	metrics       metrics
	tracer        trace.Tracer
	logger        *slog.Logger
	cfg           config.PositionWorkerConfig
	pushTimeout   time.Duration
	newTicker     func(time.Duration) Ticker
	activeWorkers atomic.Int64

	mu          sync.Mutex
	started     bool
	stopped     bool
	scanCancel  context.CancelFunc
	drainCancel context.CancelFunc
	scanWG      sync.WaitGroup
	workerWG    sync.WaitGroup
}

func New(
	repo staleTripsRepository,
	push pushClient,
	metrics metrics,
	tracerProvider trace.TracerProvider,
	logger *slog.Logger,
	cfg config.PositionWorkerConfig,
	pushTimeout time.Duration,
	newTicker func(time.Duration) Ticker,
) *Pool {
	if newTicker == nil {
		newTicker = NewTicker
	}

	queue := NewPriorityQueue(cfg.QueueSize)
	pool := &Pool{
		repo:        repo,
		push:        push,
		queue:       queue,
		metrics:     metrics,
		tracer:      tracerProvider.Tracer(tracerName),
		logger:      logger,
		cfg:         cfg,
		pushTimeout: pushTimeout,
		newTicker:   newTicker,
	}

	metrics.Observe(queue.Len, pool.activeWorkers.Load)
	return pool
}

func (p *Pool) Start(ctx context.Context) {
	p.mu.Lock()
	if p.started || p.stopped {
		p.mu.Unlock()
		return
	}

	p.started = true
	scanCtx, scanCancel := context.WithCancel(ctx)
	drainCtx, drainCancel := context.WithCancel(ctx)
	p.scanCancel = scanCancel
	p.drainCancel = drainCancel

	p.scanWG.Add(1)
	go p.runScanner(scanCtx)

	p.workerWG.Add(p.cfg.Workers)
	for range p.cfg.Workers {
		go p.runWorker(drainCtx)
	}
	p.mu.Unlock()

	p.logger.InfoContext(ctx, "position worker pool started",
		"workers", p.cfg.Workers,
		"queue_size", p.cfg.QueueSize,
		"check_interval", p.cfg.CheckInterval.String(),
		"stale_after", p.cfg.StaleAfter.String(),
		"batch_size", p.cfg.ScanBatchSize,
	)
}

func (p *Pool) Cancel(ctx context.Context, tripID uuid.UUID) {
	if p.queue.Cancel(tripID) {
		p.metrics.RequestCancelled(ctx)
	}
}

func (p *Pool) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return nil
	}

	p.stopped = true
	started := p.started
	scanCancel := p.scanCancel
	drainCancel := p.drainCancel
	p.mu.Unlock()

	if !started {
		p.queue.Close()
		return nil
	}

	scanCancel()
	p.scanWG.Wait()
	p.queue.Close()

	done := make(chan struct{})
	go func() {
		p.workerWG.Wait()
		close(done)
	}()

	var err error
	select {
	case <-done:
		drainCancel()
	case <-ctx.Done():
		drainCancel()
		<-done
		p.logger.WarnContext(ctx, "position worker pool forced shutdown", "error", ctx.Err())
		err = fmt.Errorf("shutdown position worker pool: %w", ctx.Err())
	}

	p.logger.InfoContext(ctx, "position worker pool stopped")
	return err
}

func (p *Pool) runScanner(ctx context.Context) {
	defer p.scanWG.Done()

	ticker := p.newTicker(p.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			p.scanOnce(ctx)
		}
	}
}

func (p *Pool) scanOnce(ctx context.Context) {
	tickCtx, span := p.tracer.Start(ctx, "positions.scan")
	defer span.End()

	checkSpanCtx := trace.SpanContextFromContext(tickCtx)

	scanSeq := p.queue.BeginScan()
	defer p.queue.EndScan()

	var (
		cursor    *model.StaleCursor
		totalRows int64
	)
	defer func() {
		p.metrics.RowsRead(tickCtx, totalRows)
	}()

	for {
		if tickCtx.Err() != nil {
			return
		}

		batch, err := p.repo.ListStaleBatch(tickCtx, p.cfg.StaleAfter, cursor, p.cfg.ScanBatchSize)
		if err != nil {
			if tickCtx.Err() == nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, "list stale trips failed")
				p.logger.ErrorContext(tickCtx, "list stale trips failed", "error", err)
			}
			return
		}

		totalRows += int64(len(batch))
		if len(batch) == 0 {
			return
		}

		if p.enqueueBatch(tickCtx, batch, checkSpanCtx, scanSeq) {
			return
		}

		if len(batch) < p.cfg.ScanBatchSize {
			return
		}

		last := batch[len(batch)-1]
		cursor = &model.StaleCursor{
			LastActiveAt: last.LastActiveAt,
			TripID:       last.TripID,
		}
	}
}

func (p *Pool) enqueueBatch(ctx context.Context, batch []model.StaleTrip, checkSpanCtx trace.SpanContext, scanSeq uint64) bool {
	for _, item := range batch {
		res := p.queue.Enqueue(Task{
			TripID:       item.TripID,
			DriverID:     item.DriverID,
			LastActiveAt: item.LastActiveAt,
			CheckSpanCtx: checkSpanCtx,
		}, scanSeq)

		switch res.Status {
		case StatusQueued:
			p.metrics.StaleCheck(ctx, staleResultQueued)
		case StatusDuplicate:
			p.metrics.StaleCheck(ctx, staleResultDuplicate)
		case StatusEvicted:
			p.metrics.StaleCheck(ctx, staleResultQueued)
			p.metrics.StaleCheck(ctx, staleResultDropped)
			p.logger.WarnContext(ctx, "position queue full, evicted fresher trip",
				"evicted_trip_id", res.EvictedTripID.String(),
				"queued_trip_id", item.TripID.String(),
			)
			return true
		case StatusDropped:
			p.metrics.StaleCheck(ctx, staleResultDropped)
			p.logger.WarnContext(ctx, "position queue full, dropped trip",
				"trip_id", item.TripID.String(),
			)
			return true
		case StatusCancelled:
		case StatusClosed:
			return true
		}
	}

	return false
}

func (p *Pool) runWorker(ctx context.Context) {
	defer p.workerWG.Done()

	for {
		task, ok := p.queue.Pop(ctx)
		if !ok {
			return
		}

		p.processTask(ctx, task)
	}
}

func (p *Pool) processTask(ctx context.Context, task Task) {
	p.activeWorkers.Add(1)
	defer p.activeWorkers.Add(-1)
	defer p.queue.Done(task.TripID)

	taskCtx, cancel := context.WithTimeout(ctx, p.pushTimeout)
	defer cancel()

	startOpts := []trace.SpanStartOption{
		trace.WithNewRoot(),
		trace.WithAttributes(
			attribute.String("trip.id", task.TripID.String()),
			attribute.String("driver.id", task.DriverID.String()),
		),
	}
	if task.CheckSpanCtx.IsValid() {
		startOpts = append(startOpts, trace.WithLinks(trace.Link{SpanContext: task.CheckSpanCtx}))
	}

	taskCtx, span := p.tracer.Start(taskCtx, "positions.request_position", startOpts...)
	defer span.End()

	started := time.Now()
	err := p.push.RequestPosition(taskCtx, task.TripID, task.DriverID)
	duration := time.Since(started).Seconds()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "push request failed")
		p.metrics.RequestCompleted(taskCtx, requestResultError, duration)
		p.logger.WarnContext(taskCtx, "request position push failed",
			"trip_id", task.TripID.String(),
			"driver_id", task.DriverID.String(),
			"error", err,
		)
		return
	}

	p.metrics.RequestCompleted(taskCtx, requestResultSuccess, duration)
	p.logger.DebugContext(taskCtx, "request position push sent",
		"trip_id", task.TripID.String(),
		"driver_id", task.DriverID.String(),
	)
}
