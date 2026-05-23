package sqlstore

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/messaging"
)

// TestSQLEventStore_AppendEvents 验证 SQLEventStore AppendEvents。
func TestSQLEventStore_AppendEvents(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(123)

	events := []eventing.Event[int64]{
		makeEvent(aggregateID, "TestAggregate", "event-1", 1, map[string]any{"value": 100}),
		makeEvent(aggregateID, "TestAggregate", "event-2", 2, map[string]any{"value": 200}),
	}

	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	assert.NoError(t, err)

	loaded, err := store.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 2)
	assert.Equal(t, "event-1", loaded[0].ID)
	assert.Equal(t, "event-2", loaded[1].ID)
}

// TestSQLEventStore_VersionConflict 验证 SQLEventStore VersionConflict。
func TestSQLEventStore_VersionConflict(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(456)

	events1 := []eventing.Event[int64]{makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil)}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events1), 0)
	assert.NoError(t, err)

	events2 := []eventing.Event[int64]{makeEvent(aggregateID, "TestAggregate", "event-2", 2, nil)}
	err = store.AppendEvents(ctx, aggregateID, toStorableEvents(events2), 0)
	assert.Error(t, err)
}

// TestSQLEventStore_Idempotency 验证 SQLEventStore Idempotency。
func TestSQLEventStore_Idempotency(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(789)

	events := []eventing.Event[int64]{makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil)}

	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	assert.NoError(t, err)

	_ = store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)

	loaded, err := store.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 1)
}

// TestSQLEventStore_AppendEventsWithDB 验证 SQLEventStore AppendEventsWithDB。
func TestSQLEventStore_AppendEventsWithDB(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(100)

	tx, err := database.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback()

	events := []eventing.Event[int64]{makeEvent(aggregateID, "TestAggregate", "event-tx-1", 1, nil)}
	err = store.AppendEventsWithDB(ctx, tx, aggregateID, toStorableEvents(events), 0)
	assert.NoError(t, err)

	err = tx.Commit()
	assert.NoError(t, err)

	loaded, err := store.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 1)
}

// TestSQLEventStore_EmptyEvents 验证 SQLEventStore EmptyEvents。
func TestSQLEventStore_EmptyEvents(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(500)

	err := store.AppendEvents(ctx, aggregateID, []eventing.IStorableEvent[int64]{}, 0)
	assert.NoError(t, err)

	loaded, err := store.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 0)
}

// TestSQLEventStore_MixedAggregateTypes 验证 SQLEventStore MixedAggregateTypes。
func TestSQLEventStore_MixedAggregateTypes(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(600)

	events := []eventing.Event[int64]{
		makeEvent(aggregateID, "TypeA", "event-1", 1, nil),
		makeEvent(aggregateID, "TypeB", "event-2", 2, nil),
	}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mixed aggregate types")
}

// TestSQLEventStore_VersionMismatch 验证 SQLEventStore VersionMismatch。
func TestSQLEventStore_VersionMismatch(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(700)

	events := []eventing.Event[int64]{
		makeEvent(aggregateID, "TestAggregate", "event-1", 5, nil),
	}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version mismatch")
}

// TestSQLEventStore_ConcurrencyConflict 验证 SQLEventStore ConcurrencyConflict。
func TestSQLEventStore_ConcurrencyConflict(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(999)

	events1 := []eventing.Event[int64]{makeEvent(aggregateID, "TestAggregate", "event-1", 1, nil)}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events1), 0)
	require.NoError(t, err)

	events2 := []eventing.Event[int64]{makeEvent(aggregateID, "TestAggregate", "event-2", 2, nil)}
	err = store.AppendEvents(ctx, aggregateID, toStorableEvents(events2), 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "concurrency")
}

// TestSQLEventStore_ComplexPayload 验证 SQLEventStore ComplexPayload。
func TestSQLEventStore_ComplexPayload(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	type complexPayload struct {
		Total any `json:"total"`
	}

	ctx := context.Background()
	aggregateID := int64(800)

	payload := map[string]any{
		"user": map[string]any{
			"id":   123,
			"name": "Alice",
		},
		"items": []any{
			map[string]any{"id": 1, "qty": 5},
			map[string]any{"id": 2, "qty": 3},
		},
		"total": 99.99,
	}

	events := []eventing.Event[int64]{makeEvent(aggregateID, "OrderAggregate", "OrderCreated", 1, payload)}
	err := store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0)
	assert.NoError(t, err)

	loaded, err := store.LoadEvents(ctx, aggregateID, 0)
	assert.NoError(t, err)
	assert.Len(t, loaded, 1)
	assert.NotNil(t, loaded[0].Payload)

	_, ok := messaging.PayloadAs[json.RawMessage](loaded[0].Payload)
	assert.True(t, ok)

	reg := registry.NewRegistry()
	require.NoError(t, reg.Register("TestEvent", func() any { return &complexPayload{} }))
	upgraders := upcast.NewUpgraderRegistry()
	_, err = upcast.UpgradeEventPayload(ctx, reg, upgraders, &loaded[0])
	assert.NoError(t, err)

	p, ok := messaging.PayloadAs[*complexPayload](loaded[0].Payload)
	require.True(t, ok)
	switch v := p.Total.(type) {
	case json.Number:
		f, err := v.Float64()
		assert.NoError(t, err)
		assert.InDelta(t, 99.99, f, 1e-9)
	default:
		t.Fatalf("unexpected type for payload.total: %T", p.Total)
	}
}

func TestSQLEventStore_ClassifyUniqueInsertError_Idempotent(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	aggregateID := int64(1)

	events := []eventing.Event[int64]{makeEvent(aggregateID, "TestAggregate", "event-1", 1, map[string]any{"value": 100})}
	require.NoError(t, store.AppendEvents(ctx, aggregateID, toStorableEvents(events), 0))

	encoded, err := store.codec.Encode(aggregateID)
	require.NoError(t, err)

	idempotent, classified := store.classifyUniqueInsertError(ctx, database, aggregateID, encoded, preparedEvent{
		id:            "event-1",
		typ:           "TestEvent",
		aggregateType: "TestAggregate",
		version:       1,
	}, errors.New("UNIQUE constraint failed: event_store.id"))
	require.True(t, idempotent)
	require.NoError(t, classified)
}

func TestSQLEventStore_ClassifyUniqueInsertError_EventIDTaken_Duplicate(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	require.NoError(t, store.AppendEvents(ctx, 1, toStorableEvents([]eventing.Event[int64]{
		makeEvent(1, "TestAggregate", "event-dup", 1, nil),
	}), 0))

	encoded, err := store.codec.Encode(int64(2))
	require.NoError(t, err)

	idempotent, classified := store.classifyUniqueInsertError(ctx, database, int64(2), encoded, preparedEvent{
		id:            "event-dup",
		typ:           "TestEvent",
		aggregateType: "TestAggregate",
		version:       1,
	}, errors.New("UNIQUE constraint failed: event_store.id"))
	require.False(t, idempotent)
	require.Error(t, classified)
	require.True(t, errors.Is(classified, errors.Duplicate), "expected Duplicate, got %v", classified)
}

func TestSQLEventStore_ClassifyUniqueInsertError_AggregateVersionTaken_Concurrency(t *testing.T) {
	database := setupTestDB(t)
	store := newTestStore(t, database, "event_store")

	ctx := context.Background()
	require.NoError(t, store.AppendEvents(ctx, 1, toStorableEvents([]eventing.Event[int64]{
		makeEvent(1, "TestAggregate", "event-existing", 1, nil),
	}), 0))

	encoded, err := store.codec.Encode(int64(1))
	require.NoError(t, err)

	idempotent, classified := store.classifyUniqueInsertError(ctx, database, int64(1), encoded, preparedEvent{
		id:            "event-new",
		typ:           "TestEvent",
		aggregateType: "TestAggregate",
		version:       1,
	}, errors.New("UNIQUE constraint failed: event_store.aggregate_id, event_store.aggregate_type, event_store.version"))
	require.False(t, idempotent)
	require.Error(t, classified)
	require.True(t, errors.Is(classified, errors.Concurrency), "expected Concurrency, got %v", classified)
}
