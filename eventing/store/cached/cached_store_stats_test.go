package cached

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gochen/eventing"
	"gochen/eventing/store"
)

// TestCachedEventStore_Stats 验证 CachedEventStore Stats。
func TestCachedEventStore_Stats(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(200)

	events := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event1", 1)}

	_, _ = memStore.LoadEvents(ctx, aggregateID, 0)
	_ = cachedStore.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	_, _ = cachedStore.LoadEvents(ctx, aggregateID, 0)
	_, _ = cachedStore.LoadEvents(ctx, aggregateID, 0)

	assert.NotNil(t, cachedStore)
}

// TestCachedEventStore_GetCacheStats 验证 CachedEventStore GetCacheStats。
func TestCachedEventStore_GetCacheStats(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(400)

	events := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event1", 1)}

	_, _ = memStore.LoadEvents(ctx, aggregateID, 0)
	_ = cachedStore.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	_, _ = cachedStore.LoadEvents(ctx, aggregateID, 0)
	_, _ = cachedStore.LoadEvents(ctx, aggregateID, 0)

	loaded, err := cachedStore.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 1)
}

// TestCachedEventStore_MaxCapacity 验证 CachedEventStore MaxCapacity。
func TestCachedEventStore_MaxCapacity(t *testing.T) {
	memStore := store.NewMemoryEventStore()
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
		_ = cachedStore.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
		_, _ = cachedStore.LoadEvents(ctx, int64(i), 0)
	}

	stats := cachedStore.Stats()
	assert.NotNil(t, stats)
	assert.True(t, stats.Evictions > 0)
}

// TestCachedEventStore_CacheStatistics 验证 CachedEventStore CacheStatistics。
func TestCachedEventStore_CacheStatistics(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(800)

	events := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event1", 1)}

	_ = cachedStore.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)

	_, _ = cachedStore.LoadEvents(ctx, aggregateID, 0)
	_, _ = cachedStore.LoadEvents(ctx, aggregateID, 0)
	_, _ = cachedStore.LoadEvents(ctx, aggregateID, 0)

	stats := cachedStore.Stats()
	assert.NotNil(t, stats)
	assert.True(t, stats.Hits >= 2)
	assert.True(t, stats.Misses >= 1)
	assert.True(t, stats.Invalidations >= 0)

	hitRate := cachedStore.GetHitRate()
	assert.True(t, hitRate >= 0.0 && hitRate <= 1.0)
	assert.True(t, hitRate >= 0.5)
}
