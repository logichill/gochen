package sqlstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/eventing"
	estore "gochen/eventing/store"
)

// TestSQLEventStore_StreamEvents 验证 SQLEventStore StreamEvents。
func TestSQLEventStore_StreamEvents(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()

	events1 := []eventing.Event[int64]{makeEvent(1, "TypeA", "event-1", 1, nil)}
	err := store.AppendEvents(ctx, 1, toStorableEvents(events1), 0)
	require.NoError(t, err)

	events2 := []eventing.Event[int64]{makeEvent(2, "TypeB", "event-2", 1, nil)}
	err = store.AppendEvents(ctx, 2, toStorableEvents(events2), 0)
	require.NoError(t, err)

	loaded, err := store.StreamEvents(ctx, &estore.StreamOptions{
		FromTime: events1[0].Timestamp.Add(-1 * time.Second),
		Limit:    10,
	})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(loaded.Events), 2)
}

// TestSQLEventStore_StreamEvents_PaginationAndFilters 验证 SQLEventStore StreamEvents PaginationAndFilters。
func TestSQLEventStore_StreamEvents_PaginationAndFilters(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		events := []eventing.Event[int64]{makeEvent(int64(i), "TestAggregate", fmt.Sprintf("event-%d", i), 1, nil)}
		err := store.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
		require.NoError(t, err)
	}

	t.Run("基础分页", func(t *testing.T) {
		result, err := store.StreamEvents(ctx, &estore.StreamOptions{
			Limit: 2,
		})
		assert.NoError(t, err)
		assert.Len(t, result.Events, 2)
		assert.True(t, result.HasMore)
		assert.NotEmpty(t, result.NextCursor)
	})

	t.Run("使用游标", func(t *testing.T) {
		result1, err := store.StreamEvents(ctx, &estore.StreamOptions{
			Limit: 2,
		})
		require.NoError(t, err)

		result2, err := store.StreamEvents(ctx, &estore.StreamOptions{
			After: result1.NextCursor,
			Limit: 2,
		})
		assert.NoError(t, err)
		assert.Greater(t, len(result2.Events), 0)
	})

	t.Run("按类型过滤", func(t *testing.T) {
		result, err := store.StreamEvents(ctx, &estore.StreamOptions{
			AggregateTypes: []string{"TestAggregate"},
			Limit:          10,
		})
		assert.NoError(t, err)
		assert.Greater(t, len(result.Events), 0)
		for _, evt := range result.Events {
			assert.Equal(t, "TestAggregate", evt.AggregateType)
		}
	})
}

// TestSQLEventStore_StreamEvents_TimeFilters 验证 SQLEventStore StreamEvents TimeFilters。
func TestSQLEventStore_StreamEvents_TimeFilters(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()

	now := time.Now()
	for i := 1; i <= 5; i++ {
		evt := makeEvent(int64(i), "TestAggregate", fmt.Sprintf("time-event-%d", i), 1, nil)
		evt.Timestamp = now.Add(time.Duration(i) * time.Second)
		err := store.AppendEvents(ctx, int64(i), toStorableEvents([]eventing.Event[int64]{evt}), 0)
		require.NoError(t, err)
	}

	t.Run("FromTime过滤", func(t *testing.T) {
		result, err := store.StreamEvents(ctx, &estore.StreamOptions{
			FromTime: now.Add(3 * time.Second),
			Limit:    10,
		})
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(result.Events), 3)
	})

	t.Run("ToTime过滤", func(t *testing.T) {
		result, err := store.StreamEvents(ctx, &estore.StreamOptions{
			ToTime: now.Add(3 * time.Second),
			Limit:  10,
		})
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(result.Events), 3)
	})

	t.Run("时间范围过滤", func(t *testing.T) {
		result, err := store.StreamEvents(ctx, &estore.StreamOptions{
			FromTime: now.Add(2 * time.Second),
			ToTime:   now.Add(4 * time.Second),
			Limit:    10,
		})
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(result.Events), 2)
	})
}

// TestSQLEventStore_StreamEvents_TypeFilters 验证 SQLEventStore StreamEvents TypeFilters。
func TestSQLEventStore_StreamEvents_TypeFilters(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()

	events := []eventing.Event[int64]{
		makeEvent(1, "OrderAggregate", "order-1", 1, nil),
		makeEvent(2, "UserAggregate", "user-1", 1, nil),
		makeEvent(3, "ProductAggregate", "product-1", 1, nil),
	}
	for i, evt := range events {
		evt.Type = fmt.Sprintf("Event%d", i+1)
		err := store.AppendEvents(ctx, int64(i+1), toStorableEvents([]eventing.Event[int64]{evt}), 0)
		require.NoError(t, err)
	}

	t.Run("按事件类型过滤", func(t *testing.T) {
		result, err := store.StreamEvents(ctx, &estore.StreamOptions{
			Types: []string{"Event1", "Event2"},
			Limit: 10,
		})
		assert.NoError(t, err)
		assert.Equal(t, 2, len(result.Events))
	})

	t.Run("按聚合类型过滤", func(t *testing.T) {
		result, err := store.StreamEvents(ctx, &estore.StreamOptions{
			AggregateTypes: []string{"OrderAggregate", "ProductAggregate"},
			Limit:          10,
		})
		assert.NoError(t, err)
		assert.Equal(t, 2, len(result.Events))
	})
}

// TestSQLEventStore_StreamEvents_EmptyResult 验证 SQLEventStore StreamEvents EmptyResult。
func TestSQLEventStore_StreamEvents_EmptyResult(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()

	result, err := store.StreamEvents(ctx, &estore.StreamOptions{
		Limit: 10,
	})
	assert.NoError(t, err)
	assert.Len(t, result.Events, 0)
	assert.False(t, result.HasMore)
	assert.Empty(t, result.NextCursor)
}

// TestSQLEventStore_StreamEvents_InvalidCursor 验证 SQLEventStore StreamEvents InvalidCursor。
func TestSQLEventStore_StreamEvents_InvalidCursor(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()

	events := []eventing.Event[int64]{makeEvent(1, "TestAggregate", "event-1", 1, nil)}
	err := store.AppendEvents(ctx, 1, toStorableEvents(events), 0)
	require.NoError(t, err)

	_, err = store.StreamEvents(ctx, &estore.StreamOptions{
		After: "non-existent-cursor-id",
		Limit: 10,
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, errors.NotFound))
}

func TestSQLEventStore_StreamEvents_WithOptions(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		events := []eventing.Event[int64]{makeEvent(int64(i), "TypeA", fmt.Sprintf("event-a-%d", i), 1, nil)}
		err := store.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
		require.NoError(t, err)
	}
	for i := 4; i <= 6; i++ {
		events := []eventing.Event[int64]{makeEvent(int64(i), "TypeB", fmt.Sprintf("event-b-%d", i), 1, nil)}
		err := store.AppendEvents(ctx, int64(i), toStorableEvents(events), 0)
		require.NoError(t, err)
	}

	result, err := store.StreamEvents(ctx, &estore.StreamOptions{
		AggregateTypes: []string{"TypeA"},
		Limit:          10,
	})
	assert.NoError(t, err)
	assert.Len(t, result.Events, 3)
	for _, evt := range result.Events {
		assert.Equal(t, "TypeA", evt.AggregateType)
	}
}
