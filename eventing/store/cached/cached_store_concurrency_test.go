package cached

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"gochen/eventing"
	"gochen/eventing/store"
)

// TestCachedEventStore_Concurrency 验证 CachedEventStore Concurrency。
func TestCachedEventStore_Concurrency(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()

	events1 := []eventing.Event[int64]{makeTestEvent(1, "Event1", 1)}
	events2 := []eventing.Event[int64]{makeTestEvent(2, "Event2", 1)}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = cachedStore.AppendEvents(ctx, 1, toStorableEvents(events1), 0)
	}()

	go func() {
		defer wg.Done()
		_ = cachedStore.AppendEvents(ctx, 2, toStorableEvents(events2), 0)
	}()

	wg.Wait()

	loaded1, err := cachedStore.LoadEvents(ctx, 1, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded1, 1)

	loaded2, err := cachedStore.LoadEvents(ctx, 2, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded2, 1)
}

// TestCachedEventStore_ConcurrentCacheAccess 验证 CachedEventStore ConcurrentCacheAccess。
func TestCachedEventStore_ConcurrentCacheAccess(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(900)

	events := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event1", 1)}
	_ = cachedStore.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)

	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			_, err := cachedStore.LoadEvents(ctx, aggregateID, 0)
			assert.NoError(t, err)
		}()
	}

	wg.Wait()

	stats := cachedStore.Stats()
	assert.True(t, stats.Hits > 0)
}
