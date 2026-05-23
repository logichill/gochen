package sqlstore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/logging"
)

// TestSQLEventStore_LoadEventsByType 验证 SQLEventStore LoadEventsByType。
func TestSQLEventStore_LoadEventsByType(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()

	events1 := []eventing.Event[int64]{makeEvent(1, "TypeA", "event-1", 1, nil)}
	events2 := []eventing.Event[int64]{makeEvent(1, "TypeB", "event-2", 1, nil)}

	assert.NoError(t, store.AppendEvents(ctx, 1, toStorableEvents(events1), 0))
	assert.NoError(t, store.AppendEvents(ctx, 1, toStorableEvents(events2), 0))

	loaded, err := store.LoadEventsByType(ctx, "TypeA", 1, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 1)
	assert.Equal(t, "TypeA", loaded[0].AggregateType)
}

// TestSQLEventStore_LoadEventsAfterVersion 验证 SQLEventStore LoadEventsAfterVersion。
func TestSQLEventStore_LoadEventsAfterVersion(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(400)

	events := []eventing.Event[int64]{
		makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil),
		makeEvent(aggregateID, "TestAggregate", "event-2", 2, nil),
		makeEvent(aggregateID, "TestAggregate", "event-3", 3, nil),
		makeEvent(aggregateID, "TestAggregate", "event-4", 4, nil),
		makeEvent(aggregateID, "TestAggregate", "event-5", 5, nil),
	}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	require.NoError(t, err)

	loaded, err := store.LoadEvents(ctx, aggregateID, 2)
	assert.NoError(t, err)
	assert.Len(t, loaded, 3)
	assert.Equal(t, uint64(3), loaded[0].Version)
	assert.Equal(t, uint64(5), loaded[2].Version)
}

// TestSQLEventStore_HasAggregate 验证 SQLEventStore HasAggregate。
func TestSQLEventStore_HasAggregate(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(200)

	exists, err := store.HasAggregate(ctx, aggregateID)
	assert.NoError(t, err)
	assert.False(t, exists)

	events := []eventing.Event[int64]{makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil)}
	err = store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	require.NoError(t, err)

	exists, err = store.HasAggregate(ctx, aggregateID)
	assert.NoError(t, err)
	assert.True(t, exists)
}

// TestSQLEventStore_GetAggregateVersion 验证 SQLEventStore GetAggregateVersion。
func TestSQLEventStore_GetAggregateVersion(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(300)

	version, err := store.GetAggregateVersion(ctx, aggregateID)
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), version)

	events := []eventing.Event[int64]{
		makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil),
		makeEvent(aggregateID, "TestAggregate", "event-2", 2, nil),
		makeEvent(aggregateID, "TestAggregate", "event-3", 3, nil),
	}
	err = store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	require.NoError(t, err)

	version, err = store.GetAggregateVersion(ctx, aggregateID)
	assert.NoError(t, err)
	assert.Equal(t, uint64(3), version)
}

// TestSQLEventStore_Init 验证 SQLEventStore Init。
func TestSQLEventStore_Init(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	err := store.Init(ctx)
	assert.NoError(t, err)
}

// TestSQLEventStore_Getters 验证 SQLEventStore Getters。
func TestSQLEventStore_Getters(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	assert.Equal(t, "event_store", store.GetTableName())
	assert.NotNil(t, store.GetDB())
	assert.Equal(t, database, store.GetDB())
}

// TestSQLEventStore_DefaultTableName 验证 SQLEventStore DefaultTableName。
func TestSQLEventStore_DefaultTableName(t *testing.T) {
	database := setupTestDB(t)

	store := newTestStore(t, database, "")
	assert.Equal(t, "event_store", store.GetTableName())
}

// TestSQLEventStore_TableNameValidation 验证 SQLEventStore TableNameValidation。
func TestSQLEventStore_TableNameValidation(t *testing.T) {
	database := setupTestDB(t)

	_, err := NewSQLEventStore(database, "event_store;DROP TABLE event_store;", WithLogger(logging.NewNoopLogger()))
	require.Error(t, err)
}
