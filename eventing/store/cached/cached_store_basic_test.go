package cached

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/store"
)

// TestCachedEventStore 验证 CachedEventStore。
func TestCachedEventStore(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(100)

	events := []eventing.Event[int64]{
		makeTestEvent(aggregateID, "Event1", 1),
		makeTestEvent(aggregateID, "Event2", 2),
	}

	err := cachedStore.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	assert.NoError(t, err)

	loaded, err := cachedStore.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 2)
}

// TestMemoryEventStore 验证 MemoryEventStore。
func TestMemoryEventStore(t *testing.T) {
	memStore := store.NewMemoryEventStore()

	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		events := []eventing.Event[int64]{makeTestEvent(int64(i), "Event1", 1)}
		_ = memStore.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
	}

	for i := 1; i <= 5; i++ {
		loaded, err := memStore.LoadEvents(ctx, int64(i), 0)
		assert.NoError(t, err)
		assert.Len(t, loaded, 1)
		assert.Equal(t, uint64(1), loaded[0].GetVersion())
	}
}

// TestCachedEventStore_InvalidateCache 验证 CachedEventStore InvalidateCache。
func TestCachedEventStore_InvalidateCache(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(300)

	events := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event1", 1)}

	_ = cachedStore.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	_, _ = cachedStore.LoadEvents(ctx, aggregateID, 0)

	events2 := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event2", 2)}
	_ = cachedStore.AppendEvents(ctx, aggregateID, toStorableEvents(events2), 1)

	loaded, err := cachedStore.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 2)
}

// TestCachedEventStore_LoadByType 验证 CachedEventStore LoadByType。
func TestCachedEventStore_LoadByType(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(600)

	event1 := eventing.NewEvent[int64](aggregateID, "User", "UserCreated", 1, nil)
	event2 := eventing.NewEvent[int64](aggregateID, "User", "UserUpdated", 2, nil)

	events := []eventing.IStorableEvent[int64]{event1, event2}
	_ = cachedStore.AppendEvents(ctx, aggregateID, events, 0)

	loaded, err := cachedStore.LoadEventsByType(ctx, "User", aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 2)
	assert.Equal(t, "User", loaded[0].AggregateType)
}

// TestCachedEventStore_LoadWithVersion 验证 CachedEventStore LoadWithVersion。
func TestCachedEventStore_LoadWithVersion(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(700)

	events := []eventing.Event[int64]{
		makeTestEvent(aggregateID, "Event1", 1),
		makeTestEvent(aggregateID, "Event2", 2),
		makeTestEvent(aggregateID, "Event3", 3),
	}
	_ = cachedStore.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)

	_, _ = cachedStore.LoadEvents(ctx, aggregateID, 0)

	loaded, err := cachedStore.LoadEvents(ctx, aggregateID, 1)
	assert.NoError(t, err)
	assert.Len(t, loaded, 2)
	assert.Equal(t, uint64(2), loaded[0].Version)
	assert.Equal(t, uint64(3), loaded[1].Version)
}

// TestCachedEventStore_StreamEvents 验证 CachedEventStore StreamEvents。
func TestCachedEventStore_StreamEvents(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		events := []eventing.Event[int64]{makeTestEvent(int64(i), "Event1", 1)}
		_ = cachedStore.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
	}

	fromTime := time.Now().Add(-1 * time.Hour)
	streamed, err := cachedStore.StreamEvents(ctx, &store.StreamOptions{
		FromTime: fromTime,
		Limit:    10,
	})
	assert.NoError(t, err)
	assert.True(t, len(streamed.Events) >= 3)
}

// TestCachedEventStore_StreamEvents_Filtered 验证 CachedEventStore StreamEvents Filtered。
func TestCachedEventStore_StreamEvents_Filtered(t *testing.T) {
	ctx := context.Background()
	memStore := store.NewMemoryEventStore()
	cached := NewCachedEventStore(memStore, nil)
	defer cached.Close()

	e1 := eventing.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	e2 := eventing.NewEvent[int64](1, "Agg", "TypeB", 2, nil)
	now := time.Now()
	e1.Timestamp = now
	e2.Timestamp = now

	require.NoError(t, memStore.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{e1, e2}, 0))

	res, err := cached.StreamEvents(ctx, &store.StreamOptions{
		After: e1.ID,
		Types: []string{"TypeB"},
	})
	require.NoError(t, err)
	require.Len(t, res.Events, 1)
	require.Equal(t, e2.ID, res.Events[0].ID)
	require.Equal(t, e2.ID, res.NextCursor)
	require.False(t, res.HasMore)
}

// TestCachedEventStore_ClearCache 验证 CachedEventStore ClearCache。
func TestCachedEventStore_ClearCache(t *testing.T) {
	memStore := store.NewMemoryEventStore()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		events := []eventing.Event[int64]{makeTestEvent(int64(i), "Event1", 1)}
		_ = cachedStore.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
		_, _ = cachedStore.LoadEvents(ctx, int64(i), 0)
	}

	cachedStore.ClearCache()

	initialMisses := cachedStore.Stats().Misses
	_, _ = cachedStore.LoadEvents(ctx, 1, 0)
	assert.True(t, cachedStore.Stats().Misses > initialMisses)
}
