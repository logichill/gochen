package eventing

import (
	"encoding/json"
	stderrors "errors"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/errors"
	"gochen/messaging"
)

type eventIDGeneratorFunc func() (string, error)

func (f eventIDGeneratorFunc) Next() (string, error) { return f() }

func TestNewEvent_ReturnsGeneratorError(t *testing.T) {
	generatorErr := stderrors.New("generator unavailable")

	evt, err := NewEvent[int64](eventIDGeneratorFunc(func() (string, error) {
		return "", generatorErr
	}), 1, "TestAggregate", "TestEvent", 1, nil)

	require.Nil(t, evt)
	require.ErrorIs(t, err, generatorErr)
	require.True(t, errors.Is(err, errors.Dependency))
}

func TestNewEvent_RejectsEmptyGeneratedID(t *testing.T) {
	evt, err := NewEvent[int64](eventIDGeneratorFunc(func() (string, error) {
		return "", nil
	}), 1, "TestAggregate", "TestEvent", 1, nil)

	require.Nil(t, evt)
	require.True(t, errors.Is(err, errors.Internal))
}

func TestEvent_Validate_AllowsSchemaVersionZero(t *testing.T) {
	e := &Event[int64]{
		Message: messaging.Message{
			ID:        "evt-1",
			Kind:      messaging.KindEvent,
			Type:      "TestEvent",
			Timestamp: time.Now(),
			Metadata:  messaging.NewMetadata(),
		},
		AggregateID:   1,
		AggregateType: "TestAgg",
		Version:       1,
		SchemaVersion: 0,
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got := e.EventSchemaVersion(); got != 1 {
		t.Fatalf("expected EventSchemaVersion()=1, got: %d", got)
	}
}

func TestEvent_Validate_RejectsNegativeSchemaVersion(t *testing.T) {
	e := &Event[int64]{
		Message: messaging.Message{
			ID:        "evt-1",
			Kind:      messaging.KindEvent,
			Type:      "TestEvent",
			Timestamp: time.Now(),
			Metadata:  messaging.NewMetadata(),
		},
		AggregateID:   1,
		AggregateType: "TestAgg",
		Version:       1,
		SchemaVersion: -1,
	}
	if err := e.Validate(); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestEvent_JSONShape_RemainsFlat(t *testing.T) {
	e := &Event[int64]{
		Message: messaging.Message{
			ID:        "evt-1",
			Kind:      messaging.KindEvent,
			Type:      "TestEvent",
			Timestamp: time.Unix(1710000000, 0).UTC(),
			Payload:   messaging.NewPayload(map[string]any{"value": 1}),
			Metadata:  messaging.NewMetadata(),
		},
		AggregateID:   42,
		AggregateType: "TestAgg",
		Version:       3,
		SchemaVersion: 2,
	}
	e.Metadata.Set("trace_id", "trace-1")

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal event failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal event json failed: %v", err)
	}

	for _, key := range []string{"id", "kind", "type", "timestamp", "payload", "metadata", "aggregate_id", "aggregate_type", "version", "schema_version"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("expected top-level key %q in event json", key)
		}
	}
	if _, ok := raw["Message"]; ok {
		t.Fatalf("unexpected embedded Message object in event json")
	}
}

func TestEventGetGlobalPosition(t *testing.T) {
	var nilEvent *Event[int64]
	require.Equal(t, int64(0), nilEvent.GetGlobalPosition())

	evt := NewEventWithID[int64]("evt-position", 1, "Order", "Created", 1, nil)
	evt.GlobalPosition = 42
	require.Equal(t, int64(42), evt.GetGlobalPosition())
}

func TestEventCloneMessageEnvelopeFreezesMutableFields(t *testing.T) {
	payload := map[string]any{
		"name": "original",
		"nested": map[string]any{
			"scope": "tenant-a",
		},
	}
	evt := NewEventWithID[int64]("evt-clone", 7, "Order", "Created", 3, payload)
	evt.GlobalPosition = 99
	evt.GetMetadata().Set("trace_id", "trace-1")

	cloned := evt.CloneMessageEnvelope()
	clone, ok := cloned.(*Event[int64])
	require.True(t, ok)
	require.Equal(t, evt.AggregateID, clone.AggregateID)
	require.Equal(t, evt.AggregateType, clone.AggregateType)
	require.Equal(t, evt.Version, clone.Version)
	require.Equal(t, evt.SchemaVersion, clone.SchemaVersion)
	require.Equal(t, evt.GlobalPosition, clone.GlobalPosition)

	payload["name"] = "mutated"
	payload["nested"].(map[string]any)["scope"] = "mutated"
	evt.GetMetadata().Set("trace_id", "trace-2")

	clonedPayload := messaging.PayloadValue(clone.GetPayload()).(map[string]any)
	require.Equal(t, "original", clonedPayload["name"])
	require.Equal(t, "tenant-a", clonedPayload["nested"].(map[string]any)["scope"])
	traceID, ok := clone.GetMetadata().GetString("trace_id")
	require.True(t, ok)
	require.Equal(t, "trace-1", traceID)
}
