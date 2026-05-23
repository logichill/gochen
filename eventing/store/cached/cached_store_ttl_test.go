package cached

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gochen/eventing"
	"gochen/eventing/store"
)

// TestCachedEventStore_CacheTTL 验证 CachedEventStore CacheTTL。
func TestCachedEventStore_CacheTTL(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	config := &Config{
		TTL:             100 * time.Millisecond,
		MaxAggregates:   100,
		CleanupInterval: 50 * time.Millisecond,
	}
	cachedStore := NewCachedEventStore(memStore, config)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(500)

	events := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event1", 1)}

	_ = cachedStore.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	loaded1, _ := cachedStore.LoadEvents(ctx, aggregateID, 0)
	assert.Len(t, loaded1, 1)

	time.Sleep(150 * time.Millisecond)

	loaded2, _ := cachedStore.LoadEvents(ctx, aggregateID, 0)
	assert.Len(t, loaded2, 1)
}

// TestCachedEventStore_CleanupExpiredEntries 验证 CachedEventStore CleanupExpiredEntries。
func TestCachedEventStore_CleanupExpiredEntries(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	config := &Config{
		TTL:             50 * time.Millisecond,
		MaxAggregates:   100,
		CleanupInterval: 30 * time.Millisecond,
	}
	cachedStore := NewCachedEventStore(memStore, config)
	defer cachedStore.Close()

	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		events := []eventing.Event[int64]{makeTestEvent(int64(i), "Event1", 1)}
		_ = cachedStore.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
		_, _ = cachedStore.LoadEvents(ctx, int64(i), 0)
	}

	time.Sleep(150 * time.Millisecond)

	stats := cachedStore.Stats()
	assert.NotNil(t, stats)
}

// TestCachedEventStore_DefaultConfig 验证 CachedEventStore DefaultConfig。
func TestCachedEventStore_DefaultConfig(t *testing.T) {
	config := DefaultConfig()
	assert.NotNil(t, config)
	assert.Equal(t, 5*time.Minute, config.TTL)
	assert.Equal(t, 1000, config.MaxAggregates)
	assert.Equal(t, 1*time.Minute, config.CleanupInterval)
}
