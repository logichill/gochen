package store

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/eventing"
)

func TestLoadAggregateEventsBatch_LoadsAllAggregatesWithBoundedConcurrency(t *testing.T) {
	ctx := context.Background()
	store := &batchLoadProbeStore[int64]{delay: time.Millisecond}
	aggregateIDs := []int64{1, 2, 3, 4, 5, 6, 7, 8}

	results, err := LoadAggregateEventsBatch(ctx, store, "TestAggregate", aggregateIDs, 3)
	require.NoError(t, err)
	require.Len(t, results, len(aggregateIDs))
	require.Equal(t, int32(len(aggregateIDs)), atomic.LoadInt32(&store.calls))
	require.LessOrEqual(t, atomic.LoadInt32(&store.maxActive), int32(3))
}

func TestLoadAggregateEventsBatch_StopsSchedulingWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const concurrency = 3
	aggregateIDs := make([]int64, 50)
	for i := range aggregateIDs {
		aggregateIDs[i] = int64(i + 1)
	}
	store := &batchLoadProbeStore[int64]{
		started:          make(chan struct{}, len(aggregateIDs)),
		blockUntilCancel: true,
	}

	done := make(chan error, 1)
	go func() {
		_, err := LoadAggregateEventsBatch(ctx, store, "TestAggregate", aggregateIDs, concurrency)
		done <- err
	}()

	for i := 0; i < concurrency; i++ {
		select {
		case <-store.started:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for load %d to start", i+1)
		}
	}

	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("batch load did not return after context cancellation")
	}

	require.LessOrEqual(t, atomic.LoadInt32(&store.calls), int32(concurrency))
}

type batchLoadProbeStore[ID comparable] struct {
	calls            int32
	active           int32
	maxActive        int32
	delay            time.Duration
	started          chan struct{}
	blockUntilCancel bool
}

func (s *batchLoadProbeStore[ID]) AppendEvents(context.Context, string, ID, []eventing.IStorableEvent[ID], uint64) error {
	return nil
}

func (s *batchLoadProbeStore[ID]) LoadEvents(ctx context.Context, _ string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	atomic.AddInt32(&s.calls, 1)
	active := atomic.AddInt32(&s.active, 1)
	defer atomic.AddInt32(&s.active, -1)
	s.recordMaxActive(active)
	if s.started != nil {
		s.started <- struct{}{}
	}

	if s.blockUntilCancel {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return []eventing.Event[ID]{}, nil
}

func (s *batchLoadProbeStore[ID]) HasAggregate(context.Context, string, ID) (bool, error) {
	return false, nil
}

func (s *batchLoadProbeStore[ID]) GetAggregateVersion(context.Context, string, ID) (uint64, error) {
	return 0, nil
}

func (s *batchLoadProbeStore[ID]) recordMaxActive(active int32) {
	for {
		current := atomic.LoadInt32(&s.maxActive)
		if active <= current {
			return
		}
		if atomic.CompareAndSwapInt32(&s.maxActive, current, active) {
			return
		}
	}
}
