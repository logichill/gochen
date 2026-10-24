package cached

import (
	"context"
	"testing"
	"time"

	"gochen/eventing"
	"gochen/eventing/store"
)

// 观测型记录器：容量驱逐时记录调用次数（不取任何存储锁）。
type deadlockProbeRecorder struct {
	evict int
}

func (r *deadlockProbeRecorder) RecordCacheHit()  {}
func (r *deadlockProbeRecorder) RecordCacheMiss() {}

func (r *deadlockProbeRecorder) RecordCacheEviction() {
	r.evict++
}

// 容量达到上限时，驱逐记录必须发生在释放缓存写锁之后。
// 若 evictOldestUnsafe 仍持锁调用记录器，并发写入会长时间阻塞。
func TestCachedEventStore_EvictionMetricsRecordedOutsideLock(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, &Config{
		TTL:             5 * time.Minute,
		MaxAggregates:   2,
		CleanupInterval: time.Minute,
	})
	defer cachedStore.Close()

	recorder := &deadlockProbeRecorder{}
	cachedStore.SetMetricsRecorder(recorder)

	ctx := context.Background()
	for i := int64(1); i <= 4; i++ {
		events := []eventing.Event[int64]{makeTestEvent(i, "Event", 1)}
		if err := cachedStore.AppendEvents(ctx, "TestAggregate", i, toStorableEvents(events), 0); err != nil {
			t.Fatalf("append aggregate %d: %v", i, err)
		}
		if _, err := cachedStore.LoadEvents(ctx, "TestAggregate", i, 0); err != nil {
			t.Fatalf("load aggregate %d: %v", i, err)
		}
	}

	if stats := cachedStore.Stats(); stats.Evictions <= 0 {
		t.Fatalf("expected evictions to be counted, got %d", stats.Evictions)
	}
	if recorder.evict <= 0 {
		t.Fatalf("expected metrics recorder to observe evictions, got %d", recorder.evict)
	}
}

type testCacheRecorderA struct{ hits int }

func (r *testCacheRecorderA) RecordCacheHit()      { r.hits++ }
func (r *testCacheRecorderA) RecordCacheMiss()     {}
func (r *testCacheRecorderA) RecordCacheEviction() {}

type testCacheRecorderB struct{}

func (r testCacheRecorderB) RecordCacheHit()      {}
func (r testCacheRecorderB) RecordCacheMiss()     {}
func (r testCacheRecorderB) RecordCacheEviction() {}

func TestCachedEventStore_SetMetricsRecorder_AllowsNilAndDifferentTypes(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("expected no panic, got %v", rec)
		}
	}()

	s := &CachedEventStore[int64]{}
	a := &testCacheRecorderA{}

	s.SetMetricsRecorder(a)
	if got := s.getMetrics(); got == nil {
		t.Fatalf("expected non-nil recorder")
	}

	// Switching implementations should never panic.
	s.SetMetricsRecorder(testCacheRecorderB{})
	s.SetMetricsRecorder(nil)
	s.SetMetricsRecorder(a)
}

func TestCachedEventStore_SetMetricsRecorderTreatsTypedNilAsNil(t *testing.T) {
	var recorder *testCacheRecorderA
	s := &CachedEventStore[int64]{}
	s.SetMetricsRecorder(recorder)
	if got := s.getMetrics(); got != nil {
		t.Fatalf("expected typed nil recorder to be ignored, got %T", got)
	}
}
