package positions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Gtollm/TripGO/internal/worker/positions"
)

func TestPriorityQueueOrderAndTieBreak(t *testing.T) {
	t.Parallel()

	q := positions.NewPriorityQueue(10)
	defer q.Close()

	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	oldest := positions.Task{TripID: uuid.MustParse("00000000-0000-0000-0000-000000000003"), LastActiveAt: base.Add(-3 * time.Minute)}
	tieLow := positions.Task{TripID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), LastActiveAt: base.Add(-2 * time.Minute)}
	tieHigh := positions.Task{TripID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), LastActiveAt: base.Add(-2 * time.Minute)}
	freshest := positions.Task{TripID: uuid.MustParse("00000000-0000-0000-0000-000000000004"), LastActiveAt: base.Add(-1 * time.Minute)}

	for _, task := range []positions.Task{freshest, tieHigh, oldest, tieLow} {
		require.Equal(t, positions.StatusQueued, q.Enqueue(task, 0).Status)
	}
	require.Equal(t, 4, q.Len())

	for _, want := range []positions.Task{oldest, tieLow, tieHigh, freshest} {
		got, ok := q.Pop(t.Context())
		require.True(t, ok)
		require.Equal(t, want.TripID, got.TripID)
		q.Done(got.TripID)
	}
	require.Zero(t, q.Len())
}

func TestPriorityQueueEvictionAndDrop(t *testing.T) {
	t.Parallel()

	q := positions.NewPriorityQueue(2)
	defer q.Close()

	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	mid, fresh := uuidTask(base.Add(-2*time.Minute)), uuidTask(base.Add(-1*time.Minute))
	older, tooFresh, sameAsFreshest := uuidTask(base.Add(-5*time.Minute)), uuidTask(base), uuidTask(base.Add(-2*time.Minute))

	require.Equal(t, positions.StatusQueued, q.Enqueue(mid, 0).Status)
	require.Equal(t, positions.StatusQueued, q.Enqueue(fresh, 0).Status)

	evictRes := q.Enqueue(older, 0)
	require.Equal(t, positions.StatusEvicted, evictRes.Status)
	require.Equal(t, fresh.TripID, evictRes.EvictedTripID)
	require.Equal(t, 2, q.Len())

	require.Equal(t, positions.StatusDropped, q.Enqueue(tooFresh, 0).Status)
	require.Equal(t, positions.StatusDropped, q.Enqueue(sameAsFreshest, 0).Status)

	for _, want := range []positions.Task{older, mid} {
		got, ok := q.Pop(t.Context())
		require.True(t, ok)
		require.Equal(t, want.TripID, got.TripID)
	}
}

func TestPriorityQueueDeduplicationAndCancel(t *testing.T) {
	t.Parallel()

	q := positions.NewPriorityQueue(2)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	t1, t2, t3 := uuidTask(base.Add(-2*time.Minute)), uuidTask(base.Add(-1*time.Minute)), uuidTask(base)

	require.Equal(t, positions.StatusQueued, q.Enqueue(t1, 0).Status)
	require.Equal(t, positions.StatusDuplicate, q.Enqueue(t1, 0).Status)
	require.Equal(t, positions.StatusQueued, q.Enqueue(t2, 0).Status)

	require.True(t, q.Cancel(t2.TripID))
	require.False(t, q.Cancel(t2.TripID))
	require.Equal(t, 1, q.Len())
	require.Equal(t, positions.StatusQueued, q.Enqueue(t3, 0).Status)

	popped, ok := q.Pop(t.Context())
	require.True(t, ok)
	require.Equal(t, t1.TripID, popped.TripID)
	require.Equal(t, positions.StatusDuplicate, q.Enqueue(t1, 0).Status)
	require.False(t, q.Cancel(t1.TripID))

	q.Done(t1.TripID)
	require.Equal(t, positions.StatusQueued, q.Enqueue(t1, 0).Status)

	q.Close()
	require.Equal(t, positions.StatusClosed, q.Enqueue(uuidTask(base), 0).Status)

	for _, want := range []positions.Task{t1, t3} {
		popped, ok = q.Pop(t.Context())
		require.True(t, ok)
		require.Equal(t, want.TripID, popped.TripID)
		q.Done(popped.TripID)
	}
	_, ok = q.Pop(t.Context())
	require.False(t, ok)
}

func TestPriorityQueueScanCancel(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	q := positions.NewPriorityQueue(4)
	defer q.Close()

	t1, t2 := uuidTask(base.Add(-5*time.Minute)), uuidTask(base.Add(-4*time.Minute))

	seq := q.BeginScan()
	require.Equal(t, positions.StatusQueued, q.Enqueue(t1, seq).Status)
	require.True(t, q.Cancel(t1.TripID))
	require.Equal(t, positions.StatusCancelled, q.Enqueue(t1, seq).Status)
	require.False(t, q.Cancel(t2.TripID))
	require.Equal(t, positions.StatusCancelled, q.Enqueue(t2, seq).Status)
	q.EndScan()

	seq = q.BeginScan()
	require.Equal(t, positions.StatusQueued, q.Enqueue(t2, seq).Status)
	q.EndScan()
}

func uuidTask(at time.Time) positions.Task {
	return positions.Task{TripID: uuid.New(), DriverID: uuid.New(), LastActiveAt: at}
}
