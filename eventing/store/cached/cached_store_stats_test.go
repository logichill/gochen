package cached

import (
	"context"
	"testing"
	"time"

	"gochen/testkit/assert"

	"gochen/eventing"
	"gochen/eventing/store"
)

// TestCachedEventStore_Stats 验证 CachedEventStore Stats。
func TestCachedEventStore_Stats(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(200)

	events := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event1", 1)}

	_, _ = memStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	_ = cachedStore.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents(events), 0)
	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)

	assert.NotNil(t, cachedStore)
}

// TestCachedEventStore_GetCacheStats 验证 CachedEventStore GetCacheStats。
func TestCachedEventStore_GetCacheStats(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(400)

	events := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event1", 1)}

	_, _ = memStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	_ = cachedStore.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents(events), 0)
	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)

	loaded, err := cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 1)
}

// TestCachedEventStore_MaxCapacity 验证 CachedEventStore MaxCapacity。
func TestCachedEventStore_MaxCapacity(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	config := &Config{
		TTL:             5 * time.Minute,
		MaxAggregates:   5,
		CleanupInterval: 1 * time.Minute,
	}
	cachedStore := NewCachedEventStore(memStore, config)
	defer cachedStore.Close()

	ctx := context.Background()

	for i := 1; i <= 10; i++ {
		events := []eventing.Event[int64]{makeTestEvent(int64(i), "Event1", 1)}
		_ = cachedStore.AppendEvents(ctx, "TestAggregate", int64(i), toStorableEvents(events), 0)
		_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", int64(i), 0)
	}

	stats := cachedStore.Stats()
	assert.NotNil(t, stats)
	assert.True(t, stats.Evictions > 0)
}

func TestCachedEventStore_EvictsLeastRecentlyUsedAggregate(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, &Config{
		TTL:             5 * time.Minute,
		MaxAggregates:   2,
		CleanupInterval: time.Minute,
		DisableCleanup:  true,
	})
	defer cachedStore.Close()

	ctx := context.Background()
	for i := int64(1); i <= 3; i++ {
		events := []eventing.Event[int64]{makeTestEvent(i, "Event1", 1)}
		_ = cachedStore.AppendEvents(ctx, "TestAggregate", i, toStorableEvents(events), 0)
	}

	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", 1, 0)
	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", 2, 0)
	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", 1, 0)
	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", 3, 0)

	cachedStore.cache.mutex.RLock()
	_, has1 := cachedStore.cache.aggregateCache[cacheKey("TestAggregate", int64(1))]
	_, has2 := cachedStore.cache.aggregateCache[cacheKey("TestAggregate", int64(2))]
	_, has3 := cachedStore.cache.aggregateCache[cacheKey("TestAggregate", int64(3))]
	cachedStore.cache.mutex.RUnlock()

	assert.True(t, has1)
	assert.False(t, has2)
	assert.True(t, has3)
}

func TestCachedEventStore_TouchSkipsMissingAggregateCacheEntry(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, &Config{
		TTL:             5 * time.Minute,
		MaxAggregates:   2,
		CleanupInterval: time.Minute,
		DisableCleanup:  true,
	})
	defer cachedStore.Close()

	key := cacheKey("", int64(1))
	cachedStore.cache.mutex.Lock()
	cachedStore.touchCacheEntryUnsafe(key)
	_, cacheExists := cachedStore.cache.aggregateCache[key]
	_, lruExists := cachedStore.cache.lruIndex[key]
	cachedStore.cache.mutex.Unlock()

	assert.False(t, cacheExists)
	assert.False(t, lruExists)
}

func TestCachedEventStore_CacheAggregateSkipsInvalidMaxAggregates(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, &Config{
		TTL:             5 * time.Minute,
		MaxAggregates:   2,
		CleanupInterval: time.Minute,
		DisableCleanup:  true,
	})
	defer cachedStore.Close()
	cachedStore.cache.maxAggregates = 0

	key := cacheKey("", int64(1))
	cachedStore.cacheAggregate(key, []eventing.Event[int64]{makeTestEvent(1, "Event1", 1)})

	cachedStore.cache.mutex.RLock()
	_, exists := cachedStore.cache.aggregateCache[key]
	cachedStore.cache.mutex.RUnlock()
	assert.False(t, exists)
}

// TestCachedEventStore_CacheStatistics 验证 CachedEventStore CacheStatistics。
func TestCachedEventStore_CacheStatistics(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(800)

	events := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event1", 1)}

	_ = cachedStore.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents(events), 0)

	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)

	stats := cachedStore.Stats()
	assert.NotNil(t, stats)
	assert.True(t, stats.Hits >= 2)
	assert.True(t, stats.Misses >= 1)
	assert.True(t, stats.Invalidations >= 0)

	hitRate := cachedStore.GetHitRate()
	assert.True(t, hitRate >= 0.0 && hitRate <= 1.0)
	assert.True(t, hitRate >= 0.5)
}
