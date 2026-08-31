package cached

import (
	"context"
	"gochen/eventing/internal/testutil"
	"testing"
	"time"

	"gochen/testkit/assert"
	"gochen/testkit/require"

	"gochen/clock"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/messaging"
)

// TestCachedEventStore 验证 CachedEventStore。
func TestCachedEventStore(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(100)

	events := []eventing.Event[int64]{
		makeTestEvent(aggregateID, "Event1", 1),
		makeTestEvent(aggregateID, "Event2", 2),
	}

	err := cachedStore.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents(events), 0)
	assert.NoError(t, err)

	loaded, err := cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 2)
}

func TestCacheKeySeparatesAggregateTypeAndStringID(t *testing.T) {
	require.NotEqual(t, cacheKey("a:b", "c"), cacheKey("a", "b:c"))
}

// TestMemoryEventStore 验证 MemoryEventStore。
func TestMemoryEventStore(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()

	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		events := []eventing.Event[int64]{makeTestEvent(int64(i), "Event1", 1)}
		_ = memStore.AppendEvents(ctx, "TestAggregate", int64(i), toStorableEvents(events), 0)
	}

	for i := 1; i <= 5; i++ {
		loaded, err := memStore.LoadEvents(ctx, "TestAggregate", int64(i), 0)
		assert.NoError(t, err)
		assert.Len(t, loaded, 1)
		assert.Equal(t, uint64(1), loaded[0].GetVersion())
	}
}

// TestCachedEventStore_InvalidateCache 验证 CachedEventStore InvalidateCache。
func TestCachedEventStore_InvalidateCache(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(300)

	events := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event1", 1)}

	_ = cachedStore.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents(events), 0)
	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)

	events2 := []eventing.Event[int64]{makeTestEvent(aggregateID, "Event2", 2)}
	_ = cachedStore.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents(events2), 1)

	loaded, err := cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 2)
}

// TestCachedEventStore_LoadByType 验证 CachedEventStore LoadByType。
func TestCachedEventStore_LoadByType(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(600)

	event1 := testutil.NewEvent[int64](aggregateID, "User", "UserCreated", 1, nil)
	event2 := testutil.NewEvent[int64](aggregateID, "User", "UserUpdated", 2, nil)

	events := []eventing.IStorableEvent[int64]{event1, event2}
	_ = cachedStore.AppendEvents(ctx, "User", aggregateID, events, 0)

	loaded, err := cachedStore.LoadEvents(ctx, "User", aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 2)
	assert.Equal(t, "User", loaded[0].AggregateType)
}

// TestCachedEventStore_AppendEvents_InvalidatesTypedCacheWhenFirstEventHasEmptyType
// 验证当批次首事件 aggregateType 为空、后续事件提供类型时，typed 缓存被正确失效。
// 底层 memory store 会从后续事件推断类型；若仅用首事件类型失效会漏掉 typed 缓存。
func TestCachedEventStore_AppendEvents_InvalidatesTypedCacheWhenFirstEventHasEmptyType(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(700)

	event1 := testutil.NewEvent[int64](aggregateID, "User", "UserCreated", 1, nil)
	require.NoError(t, cachedStore.AppendEvents(ctx, "User", aggregateID, []eventing.IStorableEvent[int64]{event1}, 0))

	// 填充 typed 缓存条目 "User:700"。
	loaded, err := cachedStore.LoadEvents(ctx, "User", aggregateID, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)

	// 追加一个首事件类型为空的批次；memory store 从第二个事件推断 "User"。
	event2 := testutil.NewEvent[int64](aggregateID, "", "UserUpdated", 2, nil)
	event3 := testutil.NewEvent[int64](aggregateID, "User", "UserDeleted", 3, nil)
	require.NoError(t, cachedStore.AppendEvents(ctx, "User", aggregateID, []eventing.IStorableEvent[int64]{event2, event3}, 1))

	loaded, err = cachedStore.LoadEvents(ctx, "User", aggregateID, 0)
	require.NoError(t, err)
	assert.Len(t, loaded, 3, "typed cache should be invalidated when batch infers type from later events")
}

// TestCachedEventStore_LoadEventsRejectsEmptyAggregateType 验证 cached store
// 对空 aggregateType 返回 InvalidInput，与底层 store 契约一致（避免命中通用缓存别名）。
func TestCachedEventStore_LoadEventsRejectsEmptyAggregateType(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	_, err := cachedStore.LoadEvents(ctx, "", 1, 0)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput for empty aggregate type, got %v", err)
}

// TestCachedEventStore_AppendEventsRejectsEmptyAggregateType 验证 cached store
// 写入侧也显式遵守 typed EventStore 契约，不保留无类型写入兜底。
func TestCachedEventStore_AppendEventsRejectsEmptyAggregateType(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	event := testutil.NewEvent[int64](1, "Agg", "Created", 1, nil)
	err := cachedStore.AppendEvents(ctx, " ", 1, []eventing.IStorableEvent[int64]{event}, 0)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput for empty aggregate type, got %v", err)
}

// TestCachedEventStore_LoadWithVersion 验证 CachedEventStore LoadWithVersion。
func TestCachedEventStore_LoadWithVersion(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()
	aggregateID := int64(700)

	events := []eventing.Event[int64]{
		makeTestEvent(aggregateID, "Event1", 1),
		makeTestEvent(aggregateID, "Event2", 2),
		makeTestEvent(aggregateID, "Event3", 3),
	}
	_ = cachedStore.AppendEvents(ctx, "TestAggregate", aggregateID, toStorableEvents(events), 0)

	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 0)

	loaded, err := cachedStore.LoadEvents(ctx, "TestAggregate", aggregateID, 1)
	assert.NoError(t, err)
	assert.Len(t, loaded, 2)
	assert.Equal(t, uint64(2), loaded[0].Version)
	assert.Equal(t, uint64(3), loaded[1].Version)
}

func TestCachedEventStore_ClonesCachedEvents(t *testing.T) {
	ctx := context.Background()
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, &Config{
		TTL:             time.Minute,
		MaxAggregates:   100,
		CleanupInterval: time.Hour,
	})
	defer cachedStore.Close()

	items := []map[string]any{{"name": "first"}}
	first := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, map[string]any{"items": items})
	first.GetMetadata().Set("source", "seed")
	second := testutil.NewEvent[int64](1, "Agg", "TypeB", 2, map[string]any{"name": "second"})
	second.GetMetadata().Set("source", "seed-2")

	require.NoError(t, cachedStore.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{first, second}, 0))

	loadedMiss, err := cachedStore.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Len(t, loadedMiss, 2)
	mutateLoadedEvent(loadedMiss[0])

	loadedHit, err := cachedStore.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Len(t, loadedHit, 2)
	assertLoadedEventUnchanged(t, loadedHit[0], "first", "seed")

	mutateLoadedEvent(loadedHit[0])

	loadedAfterHitMutation, err := cachedStore.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Len(t, loadedAfterHitMutation, 2)
	assertLoadedEventUnchanged(t, loadedAfterHitMutation[0], "first", "seed")

	loadedAfterVersion, err := cachedStore.LoadEvents(ctx, "Agg", 1, 1)
	require.NoError(t, err)
	require.Len(t, loadedAfterVersion, 1)
	assert.Equal(t, "second", messaging.PayloadValue(loadedAfterVersion[0].GetPayload()).(map[string]any)["name"])
	source, ok := loadedAfterVersion[0].GetMetadata().GetString("source")
	require.True(t, ok)
	assert.Equal(t, "seed-2", source)
}

func TestCachedEventStore_LoadEventsMissCachesAndReturnsClone(t *testing.T) {
	ctx := context.Background()
	inner := &loadSpyStore[int64]{
		events: []eventing.Event[int64]{*testutil.NewEvent[int64](1, "Agg", "Created", 1, map[string]any{
			"items": []map[string]any{{"name": "first"}},
		})},
	}
	inner.events[0].GetMetadata().Set("source", "seed")
	cachedStore := NewCachedEventStore[int64](inner, &Config{
		TTL:             time.Minute,
		MaxAggregates:   100,
		CleanupInterval: time.Hour,
		DisableCleanup:  true,
	})

	loaded, err := cachedStore.LoadEvents(ctx, "TestAggregate", 1, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, 1, inner.loadCalls)

	mutateLoadedEvent(loaded[0])
	assertLoadedEventUnchanged(t, inner.events[0], "first", "seed")
	hit, err := cachedStore.LoadEvents(ctx, "TestAggregate", 1, 0)
	require.NoError(t, err)
	require.Len(t, hit, 1)
	assert.Equal(t, 1, inner.loadCalls)
	assertLoadedEventUnchanged(t, hit[0], "first", "seed")
}

func TestCachedEventStore_UsesConfiguredClockForTTL(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 5, 29, 2, 0, 0, 0, time.UTC)
	clk := clock.NewManualClock(start)
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, &Config{
		TTL:             time.Minute,
		MaxAggregates:   100,
		CleanupInterval: time.Hour,
		Clock:           clk,
	})
	defer cachedStore.Close()

	event := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	require.NoError(t, cachedStore.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{event}, 0))

	_, err := cachedStore.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), cachedStore.Stats().Misses)

	clk.Advance(30 * time.Second)
	_, err = cachedStore.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), cachedStore.Stats().Hits)

	clk.Advance(31 * time.Second)
	_, err = cachedStore.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), cachedStore.Stats().Misses)
}

func mutateLoadedEvent(event eventing.Event[int64]) {
	payload := messaging.PayloadValue(event.GetPayload()).(map[string]any)
	payload["items"].([]map[string]any)[0]["name"] = "mutated"
	event.GetMetadata().Set("source", "mutated")
}

func assertLoadedEventUnchanged(t *testing.T, event eventing.Event[int64], name string, source string) {
	t.Helper()

	payload := messaging.PayloadValue(event.GetPayload()).(map[string]any)
	items := payload["items"].([]map[string]any)
	require.Len(t, items, 1)
	assert.Equal(t, name, items[0]["name"])

	gotSource, ok := event.GetMetadata().GetString("source")
	require.True(t, ok)
	assert.Equal(t, source, gotSource)
}

type loadSpyStore[ID comparable] struct {
	events    []eventing.Event[ID]
	loadCalls int
}

func (s *loadSpyStore[ID]) AppendEvents(context.Context, string, ID, []eventing.IStorableEvent[ID], uint64) error {
	return nil
}

func (s *loadSpyStore[ID]) LoadEvents(context.Context, string, ID, uint64) ([]eventing.Event[ID], error) {
	s.loadCalls++
	return s.events, nil
}

func (s *loadSpyStore[ID]) HasAggregate(context.Context, string, ID) (bool, error) {
	return len(s.events) > 0, nil
}

func (s *loadSpyStore[ID]) GetAggregateVersion(context.Context, string, ID) (uint64, error) {
	if len(s.events) == 0 {
		return 0, nil
	}
	return s.events[len(s.events)-1].GetVersion(), nil
}

func (s *loadSpyStore[ID]) StreamEvents(context.Context, *store.StreamOptions) (*store.StreamResult[ID], error) {
	return &store.StreamResult[ID]{Events: s.events}, nil
}

func (s *loadSpyStore[ID]) StreamAggregate(context.Context, *store.AggregateStreamOptions[ID]) (*store.AggregateStreamResult[ID], error) {
	return &store.AggregateStreamResult[ID]{Events: s.events}, nil
}

// TestCachedEventStore_StreamEvents 验证 CachedEventStore StreamEvents。
func TestCachedEventStore_StreamEvents(t *testing.T) {
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		events := []eventing.Event[int64]{makeTestEvent(int64(i), "Event1", 1)}
		_ = cachedStore.AppendEvents(ctx, "TestAggregate", int64(i), toStorableEvents(events), 0)
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
	memStore := store.NewMemoryEventStore[int64]()
	cached := NewCachedEventStore(memStore, nil)
	defer cached.Close()

	e1 := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "TypeB", 2, nil)
	now := time.Now()
	e1.Timestamp = now
	e2.Timestamp = now

	require.NoError(t, memStore.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1, e2}, 0))

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
	memStore := store.NewMemoryEventStore[int64]()
	cachedStore := NewCachedEventStore(memStore, nil)
	defer cachedStore.Close()

	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		events := []eventing.Event[int64]{makeTestEvent(int64(i), "Event1", 1)}
		_ = cachedStore.AppendEvents(ctx, "TestAggregate", int64(i), toStorableEvents(events), 0)
		_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", int64(i), 0)
	}

	cachedStore.ClearCache()

	initialMisses := cachedStore.Stats().Misses
	_, _ = cachedStore.LoadEvents(ctx, "TestAggregate", 1, 0)
	assert.True(t, cachedStore.Stats().Misses > initialMisses)
}
