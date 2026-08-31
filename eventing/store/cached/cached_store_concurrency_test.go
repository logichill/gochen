package cached

import (
	"context"
	"sync"
	"testing"

	"gochen/testkit/assert"
	"gochen/testkit/require"

	"gochen/eventing"
	"gochen/eventing/store"
)

type blockingLoadEventStore struct {
	store.IEventStreamStore[int64]
	loaded  chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingLoadEventStore) LoadEvents(ctx context.Context, aggregateType string, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	events, err := s.IEventStreamStore.LoadEvents(ctx, aggregateType, aggregateID, afterVersion)
	if err != nil {
		return nil, err
	}
	s.once.Do(func() {
		close(s.loaded)
		<-s.release
	})
	return events, nil
}

// TestCachedEventStore_Concurrency 验证 CachedEventStore Concurrency。
func TestCachedEventStore_Concurrency(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()

	events1 := []eventing.Event[int64]{makeTestEvent(1, "Event1", 1)}
	events2 := []eventing.Event[int64]{makeTestEvent(2, "Event2", 1)}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = cachedStore.AppendEvents(ctx, "TestAggregate", 1, toStorableEvents(events1), 0)
	}()

	go func() {
		defer wg.Done()
		_ = cachedStore.AppendEvents(ctx, "TestAggregate", 2, toStorableEvents(events2), 0)
	}()

	wg.Wait()

	loaded1, err := cachedStore.LoadEvents(ctx, "TestAggregate", 1, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded1, 1)

	loaded2, err := cachedStore.LoadEvents(ctx, "TestAggregate", 2, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded2, 1)
}

// TestCachedEventStore_ConcurrentCacheAccess 验证 CachedEventStore ConcurrentCacheAccess。
func TestCachedEventStore_ConcurrentCacheAccess(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(900)

	events := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event1", 1)}
	require.NoError(t, cachedStore.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents(events), 0))
	_, err := cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	require.NoError(t, err)
	initialHits := cachedStore.Stats().Hits

	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			_, err := cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
			assert.NoError(t, err)
		}()
	}

	wg.Wait()

	stats := cachedStore.Stats()
	assert.Equal(t, initialHits+10, stats.Hits)
}

func TestCachedEventStore_DoesNotRefillStaleSnapshotAfterAppend(t *testing.T) {
	ctx := context.Background()
	inner := store.NewMemoryEventStore[int64]()
	aggregateID := int64(901)
	first := makeTestEvent(aggregateID, "Event1", 1)
	require.NoError(t, inner.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents([]eventing.Event[int64]{first}), 0))

	blocking := &blockingLoadEventStore{
		IEventStreamStore: inner,
		loaded:            make(chan struct{}),
		release:           make(chan struct{}),
	}
	cachedStore := NewCachedEventStore[int64](blocking, &Config{DisableCleanup: true})
	defer cachedStore.Close()

	type loadResult struct {
		events []eventing.Event[int64]
		err    error
	}
	loadDone := make(chan loadResult, 1)
	go func() {
		events, err := cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
		loadDone <- loadResult{events: events, err: err}
	}()
	<-blocking.loaded

	second := makeTestEvent(aggregateID, "Event2", 2)
	require.NoError(t, cachedStore.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents([]eventing.Event[int64]{second}), 1))
	close(blocking.release)
	stale := <-loadDone
	require.NoError(t, stale.err)
	require.Len(t, stale.events, 1)

	latest, err := cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	require.NoError(t, err)
	require.Len(t, latest, 2)
	require.Equal(t, uint64(2), latest[1].Version)
}

func TestCachedEventStore_DoesNotRefillStaleSnapshotAfterClearCache(t *testing.T) {
	ctx := context.Background()
	inner := store.NewMemoryEventStore[int64]()
	aggregateID := int64(902)
	first := makeTestEvent(aggregateID, "Event1", 1)
	require.NoError(t, inner.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents([]eventing.Event[int64]{first}), 0))

	blocking := &blockingLoadEventStore{
		IEventStreamStore: inner,
		loaded:            make(chan struct{}),
		release:           make(chan struct{}),
	}
	cachedStore := NewCachedEventStore[int64](blocking, &Config{DisableCleanup: true})
	defer cachedStore.Close()

	type loadResult struct {
		events []eventing.Event[int64]
		err    error
	}
	loadDone := make(chan loadResult, 1)
	go func() {
		events, err := cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
		loadDone <- loadResult{events: events, err: err}
	}()
	<-blocking.loaded

	second := makeTestEvent(aggregateID, "Event2", 2)
	require.NoError(t, inner.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents([]eventing.Event[int64]{second}), 1))
	cachedStore.ClearCache()
	close(blocking.release)
	stale := <-loadDone
	require.NoError(t, stale.err)
	require.Len(t, stale.events, 1)

	latest, err := cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	require.NoError(t, err)
	require.Len(t, latest, 2)
	require.Equal(t, uint64(2), latest[1].Version)
}
