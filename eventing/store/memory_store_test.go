package store

import (
	"context"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/internal/testutil"
	"gochen/messaging"
)

func TestMemoryEventStoreZeroValueSupportsAppendAndRead(t *testing.T) {
	var memoryStore MemoryEventStore[int64]
	event := testutil.NewEvent[int64](1, "Agg", "Created", 1, nil)

	require.NoError(t, memoryStore.AppendEvents(
		context.Background(),
		"Agg",
		1,
		[]eventing.IStorableEvent[int64]{event},
		0,
	))
	loaded, err := memoryStore.LoadEvents(context.Background(), "Agg", 1, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, event.GetID(), loaded[0].GetID())

	streamed, err := memoryStore.StreamEvents(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, streamed.Events, 1)
}

func TestMemoryEventStoreHonorsContextAcrossPublicOperations(t *testing.T) {
	memoryStore := NewMemoryEventStore[int64]()
	event := testutil.NewEvent[int64](1, "Agg", "Created", 1, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		call func(context.Context) error
	}{
		{
			name: "append",
			call: func(ctx context.Context) error {
				return memoryStore.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{event}, 0)
			},
		},
		{
			name: "load",
			call: func(ctx context.Context) error {
				_, err := memoryStore.LoadEvents(ctx, "Agg", 1, 0)
				return err
			},
		},
		{
			name: "stream aggregate",
			call: func(ctx context.Context) error {
				_, err := memoryStore.StreamAggregate(ctx, &AggregateStreamOptions[int64]{AggregateType: "Agg", AggregateID: 1})
				return err
			},
		},
		{
			name: "stream events",
			call: func(ctx context.Context) error {
				_, err := memoryStore.StreamEvents(ctx, nil)
				return err
			},
		},
		{
			name: "has aggregate",
			call: func(ctx context.Context) error {
				_, err := memoryStore.HasAggregate(ctx, "Agg", 1)
				return err
			},
		},
		{
			name: "aggregate version",
			call: func(ctx context.Context) error {
				_, err := memoryStore.GetAggregateVersion(ctx, "Agg", 1)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name+" canceled", func(t *testing.T) {
			require.ErrorIs(t, test.call(ctx), context.Canceled)
		})
		t.Run(test.name+" nil", func(t *testing.T) {
			require.True(t, errors.Is(test.call(nil), errors.InvalidInput))
		})
	}
}

// TestMemoryEventStore_StreamEvents_FilterByAfterAndType 验证 MemoryEventStore StreamEvents FilterByAfterAndType。
func TestMemoryEventStore_StreamEvents_FilterByAfterAndType(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	e1 := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "TypeB", 2, nil)
	e3 := testutil.NewEvent[int64](1, "Agg", "TypeA", 3, nil)
	e1.ID = "event-1"
	e2.ID = "event-2"
	e3.ID = "event-3"

	// 统一时间戳，验证同时间戳下的 ID 游标过滤
	now := time.Now()
	e1.Timestamp = now
	e2.Timestamp = now
	e3.Timestamp = now

	require.NoError(t, store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1, e2, e3}, 0))

	result, err := store.StreamEvents(ctx, &StreamOptions{
		After: e1.ID,
		Types: []string{"TypeA"},
		Limit: 10,
	})
	require.NoError(t, err)

	require.Len(t, result.Events, 1)
	require.Equal(t, e3.ID, result.Events[0].ID)
	require.False(t, result.HasMore)
	require.Equal(t, e3.ID, result.NextCursor)
}

func TestMemoryEventStore_StreamEvents_UnknownCursorFailsFast(t *testing.T) {
	store := NewMemoryEventStore[int64]()
	_, err := store.StreamEvents(context.Background(), &StreamOptions{After: "missing-cursor"})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.NotFound))
}

func TestMemoryEventStore_AppendEvents_RejectsNilEvents(t *testing.T) {
	store := NewMemoryEventStore[int64]()
	var typedNil *eventing.Event[int64]
	for _, event := range []eventing.IStorableEvent[int64]{nil, typedNil} {
		err := store.AppendEvents(context.Background(), "Agg", 1, []eventing.IStorableEvent[int64]{event}, 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, errors.InvalidInput))
	}
}

func TestMemoryEventStore_StreamEvents_UsesGlobalTimestampOrderAcrossAppends(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	base := time.Now()
	later := testutil.NewEvent[int64](2, "Agg", "TypeA", 1, nil)
	later.ID = "event-2"
	later.Timestamp = base.Add(2 * time.Second)
	earlier := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	earlier.ID = "event-1"
	earlier.Timestamp = base.Add(time.Second)

	require.NoError(t, store.AppendEvents(ctx, "Agg", 2, []eventing.IStorableEvent[int64]{later}, 0))
	require.NoError(t, store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{earlier}, 0))

	result, err := store.StreamEvents(ctx, &StreamOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, result.Events, 2)
	require.Equal(t, "event-1", result.Events[0].ID)
	require.Equal(t, "event-2", result.Events[1].ID)

	next, err := store.StreamEvents(ctx, &StreamOptions{After: "event-1", Limit: 10})
	require.NoError(t, err)
	require.Len(t, next.Events, 1)
	require.Equal(t, "event-2", next.Events[0].ID)
}

func TestMemoryEventStore_StreamEvents_UsesGlobalIDOrderForSameTimestampAcrossAggregates(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	ts := time.Now()
	laterID := testutil.NewEvent[int64](2, "Agg", "TypeA", 1, nil)
	laterID.ID = "event-2"
	laterID.Timestamp = ts
	earlierID := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	earlierID.ID = "event-1"
	earlierID.Timestamp = ts

	require.NoError(t, store.AppendEvents(ctx, "Agg", 2, []eventing.IStorableEvent[int64]{laterID}, 0))
	require.NoError(t, store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{earlierID}, 0))

	result, err := store.StreamEvents(ctx, &StreamOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, result.Events, 2)
	require.Equal(t, "event-1", result.Events[0].ID)
	require.Equal(t, "event-2", result.Events[1].ID)
	require.Equal(t, []string{"event-1", "event-2"}, result.EventCursors)
}

func TestMemoryEventStore_AppendEvents_DuplicateSameEventsIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	e1 := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "TypeB", 2, nil)
	events := []eventing.IStorableEvent[int64]{e1, e2}

	require.NoError(t, store.AppendEvents(ctx, "Agg", 1, events, 0))
	require.NoError(t, store.AppendEvents(ctx, "Agg", 1, events, 0))

	loaded, err := store.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	require.Equal(t, e1.GetID(), loaded[0].GetID())
	require.Equal(t, e2.GetID(), loaded[1].GetID())
}

func TestMemoryEventStore_AppendEvents_ReplayedMiddleSegmentDuplicateConflicts(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	e1 := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "TypeB", 2, nil)
	e3 := testutil.NewEvent[int64](1, "Agg", "TypeC", 3, nil)
	require.NoError(t, store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1, e2, e3}, 0))

	err := store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e2}, 1)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Duplicate), "expected duplicate error, got %v", err)

	loaded, loadErr := store.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, loadErr)
	require.Len(t, loaded, 3)
}

func TestMemoryEventStore_AppendEvents_RejectsAggregateIDMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	evt := testutil.NewEvent[int64](2, "Agg", "TypeA", 1, nil)
	err := store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{evt}, 0)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.InvalidInput), "expected invalid input, got %v", err)

	loaded, loadErr := store.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, loadErr)
	require.Empty(t, loaded)
}

func TestMemoryEventStore_AppendEvents_RejectsMixedAggregateTypes(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	typeA := testutil.NewEvent[int64](1, "TypeA", "A1", 1, nil)
	typeB := testutil.NewEvent[int64](1, "TypeB", "B2", 2, nil)

	err := store.AppendEvents(ctx, "TypeA", 1, []eventing.IStorableEvent[int64]{typeA, typeB}, 0)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.InvalidInput), "expected invalid input, got %v", err)

	loadedA, loadErr := store.LoadEvents(ctx, "TypeA", 1, 0)
	require.NoError(t, loadErr)
	require.Empty(t, loadedA)
	loadedB, loadErr := store.LoadEvents(ctx, "TypeB", 1, 0)
	require.NoError(t, loadErr)
	require.Empty(t, loadedB)
}

func TestMemoryEventStore_AppendEvents_FillsEmptyAggregateTypeBeforeValidate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	typed := testutil.NewEvent[int64](1, "TypeA", "A1", 1, nil)
	untyped := testutil.NewEvent[int64](1, "", "A2", 2, nil)

	require.NoError(t, store.AppendEvents(ctx, "TypeA", 1, []eventing.IStorableEvent[int64]{typed, untyped}, 0))

	loaded, err := store.LoadEvents(ctx, "TypeA", 1, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	require.Empty(t, untyped.GetAggregateType())
	require.Equal(t, "TypeA", loaded[1].GetAggregateType())
}

func TestMemoryEventStore_LoadEventsRejectsEmptyAggregateType(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	_, err := store.LoadEvents(ctx, "", 1, 0)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput for empty aggregate type, got %v", err)

	_, err = store.LoadEvents(ctx, "  ", 1, 0)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput for whitespace aggregate type, got %v", err)
}

func TestMemoryEventStore_NormalizesAggregateTypeAcrossOperations(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()
	evt := testutil.NewEvent[int64](1, " Agg ", "Created", 1, nil)

	require.NoError(t, store.AppendEvents(ctx, " Agg ", 1, []eventing.IStorableEvent[int64]{evt}, 0))

	loaded, err := store.LoadEvents(ctx, " Agg ", 1, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, "Agg", loaded[0].GetAggregateType())

	version, err := store.GetAggregateVersion(ctx, " Agg ", 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), version)

	streamed, err := store.StreamAggregate(ctx, &AggregateStreamOptions[int64]{AggregateType: " Agg ", AggregateID: 1})
	require.NoError(t, err)
	require.Len(t, streamed.Events, 1)
}

func TestMemoryEventStore_AllowsReusingAggregateIDAcrossTypes(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	typeB := testutil.NewEvent[int64](1, "TypeB", "B1", 1, nil)
	typeBNext := testutil.NewEvent[int64](1, "TypeB", "B2", 2, nil)
	typeA := testutil.NewEvent[int64](1, "TypeA", "A1", 1, nil)

	require.NoError(t, store.AppendEvents(ctx, "TypeB", 1, []eventing.IStorableEvent[int64]{typeB, typeBNext}, 0))
	require.NoError(t, store.AppendEvents(ctx, "TypeA", 1, []eventing.IStorableEvent[int64]{typeA}, 0))

	loadedB, err := store.LoadEvents(ctx, "TypeB", 1, 0)
	require.NoError(t, err)
	require.Len(t, loadedB, 2)
	version, err := store.GetAggregateVersion(ctx, "TypeB", 1)
	require.NoError(t, err)
	require.Equal(t, uint64(2), version)

	loadedA, err := store.LoadEvents(ctx, "TypeA", 1, 0)
	require.NoError(t, err)
	require.Len(t, loadedA, 1)
	version, err = store.GetAggregateVersion(ctx, "TypeA", 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), version)

	exists, err := store.HasAggregate(ctx, "TypeA", 1)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestMemoryEventStore_AppendEvents_DuplicateIDWithDifferentContentConflicts(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	e1 := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, map[string]any{"value": "original"})
	require.NoError(t, store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0))

	conflicting := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, map[string]any{"value": "changed"})
	conflicting.ID = e1.ID
	conflicting.Timestamp = e1.Timestamp
	err := store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{conflicting}, 0)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Duplicate), "expected duplicate error, got %v", err)

	loaded, err := store.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	payload := messaging.PayloadValue(loaded[0].GetPayload()).(map[string]any)
	require.Equal(t, "original", payload["value"])
}

func TestMemoryEventStore_AppendEvents_DuplicateIDAcrossAggregatesConflicts(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	e1 := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	require.NoError(t, store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0))

	conflicting := testutil.NewEvent[int64](2, "Agg", "TypeA", 1, nil)
	conflicting.ID = e1.ID
	err := store.AppendEvents(ctx, "Agg", 2, []eventing.IStorableEvent[int64]{conflicting}, 0)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Duplicate), "expected duplicate error, got %v", err)

	stream, err := store.StreamEvents(ctx, &StreamOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, stream.Events, 1)
	require.Equal(t, e1.ID, stream.Events[0].ID)
}

func TestMemoryEventStore_AppendEvents_DuplicateIDWithinBatchConflicts(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	e1 := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "TypeA", 2, nil)
	e2.ID = e1.ID

	err := store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1, e2}, 0)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Duplicate), "expected duplicate error, got %v", err)

	stream, streamErr := store.StreamEvents(ctx, &StreamOptions{Limit: 10})
	require.NoError(t, streamErr)
	require.Empty(t, stream.Events)
}

func TestMemoryEventStore_LoadAndAppendUseIsolatedCopies(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	payload := map[string]any{
		"name": "original",
		"nested": map[string]any{
			"scope": "tenant-a",
		},
	}
	e1 := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, payload)
	e1.GetMetadata().Set("source", "seed")

	require.NoError(t, store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0))

	payload["name"] = "mutated-after-append"
	payload["nested"].(map[string]any)["scope"] = "mutated-after-append"
	e1.GetMetadata().Set("source", "mutated-after-append")

	loaded, err := store.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	loadedPayload := messaging.PayloadValue(loaded[0].GetPayload()).(map[string]any)
	require.Equal(t, "original", loadedPayload["name"])
	require.Equal(t, "tenant-a", loadedPayload["nested"].(map[string]any)["scope"])
	source, ok := loaded[0].GetMetadata().GetString("source")
	require.True(t, ok)
	require.Equal(t, "seed", source)

	loadedPayload["name"] = "mutated-after-load"
	loadedPayload["nested"].(map[string]any)["scope"] = "mutated-after-load"
	loaded[0].GetMetadata().Set("source", "mutated-after-load")

	loadedAgain, err := store.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Len(t, loadedAgain, 1)
	loadedAgainPayload := messaging.PayloadValue(loadedAgain[0].GetPayload()).(map[string]any)
	require.Equal(t, "original", loadedAgainPayload["name"])
	require.Equal(t, "tenant-a", loadedAgainPayload["nested"].(map[string]any)["scope"])
	source, ok = loadedAgain[0].GetMetadata().GetString("source")
	require.True(t, ok)
	require.Equal(t, "seed", source)
}

func TestMemoryEventStore_ClonePayloadPreservesNilMapSliceElements(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	items := []map[string]any{nil, {"name": "original"}}
	e1 := testutil.NewEvent[int64](1, "Agg", "TypeA", 1, map[string]any{"items": items})

	require.NoError(t, store.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0))

	items[1]["name"] = "mutated"

	loaded, err := store.LoadEvents(ctx, "Agg", 1, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	payload := messaging.PayloadValue(loaded[0].GetPayload()).(map[string]any)
	gotItems := payload["items"].([]map[string]any)
	require.Nil(t, gotItems[0])
	require.Equal(t, "original", gotItems[1]["name"])
}

func TestMemoryEventStore_StreamEvents_ClampsOversizedLimit(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	for i := 1; i <= MaxStreamLimit+5; i++ {
		evt := testutil.NewEvent[int64](int64(i), "Agg", "TypeA", 1, nil)
		evt.ID = time.Now().Add(time.Duration(i) * time.Millisecond).Format(time.RFC3339Nano)
		require.NoError(t, store.AppendEvents(ctx, "Agg", int64(i), []eventing.IStorableEvent[int64]{evt}, 0))
	}

	result, err := store.StreamEvents(ctx, &StreamOptions{Limit: MaxStreamLimit + 500})
	require.NoError(t, err)
	require.Len(t, result.Events, MaxStreamLimit)
	require.True(t, result.HasMore)
}

func TestMemoryEventStore_StreamAggregate_ClampsOversizedLimit(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	events := make([]eventing.IStorableEvent[int64], 0, MaxStreamLimit+5)
	for i := 1; i <= MaxStreamLimit+5; i++ {
		events = append(events, testutil.NewEvent[int64](1, "Agg", "TypeA", uint64(i), nil))
	}
	require.NoError(t, store.AppendEvents(ctx, "Agg", 1, events, 0))

	result, err := store.StreamAggregate(ctx, &AggregateStreamOptions[int64]{
		AggregateType: "Agg",
		AggregateID:   1,
		Limit:         MaxStreamLimit + 500,
	})
	require.NoError(t, err)
	require.Len(t, result.Events, MaxStreamLimit)
	require.True(t, result.HasMore)
}

func TestMemoryEventStore_StreamAggregate_RequiresAggregateType(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	_, err := store.StreamAggregate(ctx, &AggregateStreamOptions[int64]{AggregateID: 1})
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.InvalidInput), "expected invalid input, got %v", err)
}

func TestMemoryEventStore_GetAggregateVersionRequiresAggregateType(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[int64]()

	for _, aggregateType := range []string{"", "   "} {
		_, err := store.GetAggregateVersion(ctx, aggregateType, 1)
		require.Error(t, err)
		require.Truef(t, errors.Is(err, errors.InvalidInput), "expected invalid input, got %v", err)
	}
}

func TestStringMemoryEventStoreNormalizesAggregateTypeAcrossOperations(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[string]()
	event := eventing.NewEventWithID("event-1", "account-1", " Account ", "Opened", 1, map[string]any{"balance": 10})

	require.NoError(t, store.AppendEvents(ctx, " Account ", "account-1", []eventing.IStorableEvent[string]{event}, 0))
	require.Equal(t, " Account ", event.GetAggregateType(), "append must not mutate the caller's event")

	loaded, err := store.LoadEvents(ctx, "Account", "account-1", 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, "Account", loaded[0].GetAggregateType())

	exists, err := store.HasAggregate(ctx, " Account ", "account-1")
	require.NoError(t, err)
	require.True(t, exists)

	version, err := store.GetAggregateVersion(ctx, "Account", "account-1")
	require.NoError(t, err)
	require.Equal(t, uint64(1), version)

	stream, err := store.StreamAggregate(ctx, &AggregateStreamOptions[string]{
		AggregateType: " Account ",
		AggregateID:   "account-1",
	})
	require.NoError(t, err)
	require.Len(t, stream.Events, 1)
	require.Equal(t, "Account", stream.Events[0].GetAggregateType())
}

func TestStringMemoryEventStoreRejectsBlankAggregateType(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[string]()

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "append", run: func() error { return store.AppendEvents(ctx, "  ", "account-1", nil, 0) }},
		{name: "load", run: func() error { _, err := store.LoadEvents(ctx, "  ", "account-1", 0); return err }},
		{name: "has", run: func() error { _, err := store.HasAggregate(ctx, "  ", "account-1"); return err }},
		{name: "version", run: func() error { _, err := store.GetAggregateVersion(ctx, "  ", "account-1"); return err }},
		{name: "stream", run: func() error {
			_, err := store.StreamAggregate(ctx, &AggregateStreamOptions[string]{AggregateType: "  ", AggregateID: "account-1"})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			require.Truef(t, errors.Is(err, errors.InvalidInput), "error = %v", err)
		})
	}
}

func TestStringMemoryEventStoreScopesSameIDByAggregateType(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore[string]()
	aggregateID := "shared-id"

	userEvent := eventing.NewEventWithID("user-event", aggregateID, "User", "Created", 1, nil)
	orderEvent := eventing.NewEventWithID("order-event", aggregateID, "Order", "Created", 1, nil)
	require.NoError(t, store.AppendEvents(ctx, "User", aggregateID, []eventing.IStorableEvent[string]{userEvent}, 0))
	require.NoError(t, store.AppendEvents(ctx, "Order", aggregateID, []eventing.IStorableEvent[string]{orderEvent}, 0))

	users, err := store.LoadEvents(ctx, "User", aggregateID, 0)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, "user-event", users[0].GetID())

	orders, err := store.LoadEvents(ctx, "Order", aggregateID, 0)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	require.Equal(t, "order-event", orders[0].GetID())
}
