package eventing

import (
	"encoding/json"
	"testing"
	"time"

	"gochen/messaging"
)

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
