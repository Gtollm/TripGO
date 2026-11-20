package positions

import (
	"bytes"
	"container/heap"
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type EnqueueStatus uint8

const (
	StatusQueued EnqueueStatus = iota
	StatusDuplicate
	StatusEvicted
	StatusDropped
	StatusCancelled
	StatusClosed
)

type EnqueueResult struct {
	Status        EnqueueStatus
	EvictedTripID uuid.UUID
}

type Task struct {
	TripID       uuid.UUID
	DriverID     uuid.UUID
	LastActiveAt time.Time
	CheckSpanCtx trace.SpanContext
}

type queueItem struct {
	task   Task
	minIdx int
	maxIdx int
}

type PriorityQueue struct {
	mu        sync.Mutex
	minH      minHeap
	maxH      maxHeap
	queued    map[uuid.UUID]*queueItem
	running   map[uuid.UUID]struct{}
	cancelled map[uuid.UUID]uint64
	scanSeq   uint64
	scanning  bool
	capacity  int
	closed    bool
	readyCh   chan struct{}
	closedCh  chan struct{}
}

func NewPriorityQueue(capacity int) *PriorityQueue {
	if capacity <= 0 {
		capacity = 1
	}

	return &PriorityQueue{
		minH:      make(minHeap, 0, capacity),
		maxH:      make(maxHeap, 0, capacity),
		queued:    make(map[uuid.UUID]*queueItem, capacity),
		running:   make(map[uuid.UUID]struct{}),
		cancelled: make(map[uuid.UUID]uint64),
		capacity:  capacity,
		readyCh:   make(chan struct{}, 1),
		closedCh:  make(chan struct{}),
	}
}

func (q *PriorityQueue) BeginScan() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.scanning = true
	return q.scanSeq
}

func (q *PriorityQueue) EndScan() {
	q.mu.Lock()
	q.scanning = false
	clear(q.cancelled)
	q.mu.Unlock()
}

func (q *PriorityQueue) Enqueue(task Task, scanSeq uint64) EnqueueResult {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return EnqueueResult{Status: StatusClosed}
	}

	if _, ok := q.queued[task.TripID]; ok {
		return EnqueueResult{Status: StatusDuplicate}
	}

	if _, ok := q.running[task.TripID]; ok {
		return EnqueueResult{Status: StatusDuplicate}
	}

	if cancelSeq, ok := q.cancelled[task.TripID]; ok && cancelSeq > scanSeq {
		return EnqueueResult{Status: StatusCancelled}
	}

	if len(q.minH) < q.capacity {
		q.pushLocked(task)
		q.notifyReadyLocked()
		return EnqueueResult{Status: StatusQueued}
	}

	freshest := q.maxH[0]
	if task.LastActiveAt.Before(freshest.task.LastActiveAt) {
		evictedID := freshest.task.TripID
		q.removeLocked(freshest)
		q.pushLocked(task)
		q.notifyReadyLocked()
		return EnqueueResult{
			Status:        StatusEvicted,
			EvictedTripID: evictedID,
		}
	}

	return EnqueueResult{Status: StatusDropped}
}

func (q *PriorityQueue) Pop(ctx context.Context) (Task, bool) {
	for {
		q.mu.Lock()
		if ctx.Err() != nil {
			q.mu.Unlock()
			return Task{}, false
		}

		if len(q.minH) > 0 {
			item, _ := heap.Pop(&q.minH).(*queueItem)
			heap.Remove(&q.maxH, item.maxIdx)
			delete(q.queued, item.task.TripID)
			q.running[item.task.TripID] = struct{}{}
			if len(q.minH) > 0 {
				q.notifyReadyLocked()
			}
			q.mu.Unlock()
			return item.task, true
		}

		if q.closed {
			q.mu.Unlock()
			return Task{}, false
		}
		q.mu.Unlock()

		select {
		case <-ctx.Done():
			return Task{}, false
		case <-q.closedCh:
		case <-q.readyCh:
		}
	}
}

func (q *PriorityQueue) Done(tripID uuid.UUID) {
	q.mu.Lock()
	delete(q.running, tripID)
	q.mu.Unlock()
}

func (q *PriorityQueue) Cancel(tripID uuid.UUID) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	item, ok := q.queued[tripID]
	if ok {
		q.removeLocked(item)
	}

	if q.scanning {
		q.scanSeq++
		q.cancelled[tripID] = q.scanSeq
	}

	return ok
}

func (q *PriorityQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.closed {
		q.closed = true
		close(q.closedCh)
	}
}

func (q *PriorityQueue) Len() int {
	q.mu.Lock()
	n := len(q.minH)
	q.mu.Unlock()
	return n
}

func (q *PriorityQueue) pushLocked(task Task) {
	item := &queueItem{
		task:   task,
		minIdx: -1,
		maxIdx: -1,
	}
	heap.Push(&q.minH, item)
	heap.Push(&q.maxH, item)
	q.queued[task.TripID] = item
}

func (q *PriorityQueue) removeLocked(item *queueItem) {
	heap.Remove(&q.minH, item.minIdx)
	heap.Remove(&q.maxH, item.maxIdx)
	delete(q.queued, item.task.TripID)
}

func (q *PriorityQueue) notifyReadyLocked() {
	select {
	case q.readyCh <- struct{}{}:
	default:
	}
}

type minHeap []*queueItem

func (h minHeap) Len() int { return len(h) }

func (h minHeap) Less(i, j int) bool {
	if !h[i].task.LastActiveAt.Equal(h[j].task.LastActiveAt) {
		return h[i].task.LastActiveAt.Before(h[j].task.LastActiveAt)
	}

	return bytes.Compare(h[i].task.TripID[:], h[j].task.TripID[:]) < 0
}

func (h minHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].minIdx = i
	h[j].minIdx = j
}

func (h *minHeap) Push(x any) {
	item, _ := x.(*queueItem)
	item.minIdx = len(*h)
	*h = append(*h, item)
}

func (h *minHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.minIdx = -1
	*h = old[:n-1]
	return item
}

type maxHeap []*queueItem

func (h maxHeap) Len() int { return len(h) }

func (h maxHeap) Less(i, j int) bool {
	if !h[i].task.LastActiveAt.Equal(h[j].task.LastActiveAt) {
		return h[i].task.LastActiveAt.After(h[j].task.LastActiveAt)
	}

	return bytes.Compare(h[i].task.TripID[:], h[j].task.TripID[:]) > 0
}

func (h maxHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].maxIdx = i
	h[j].maxIdx = j
}

func (h *maxHeap) Push(x any) {
	item, _ := x.(*queueItem)
	item.maxIdx = len(*h)
	*h = append(*h, item)
}

func (h *maxHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.maxIdx = -1
	*h = old[:n-1]
	return item
}
