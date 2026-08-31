package messaging

import (
	"testing"
	"time"
)

func TestCloneValueCopiesSupportedMutableShapes(t *testing.T) {
	items := []map[string]any{nil, {"name": "original"}}
	value := map[string]any{
		"items": items,
		"bytes": []byte("abc"),
	}

	cloned := CloneValue(value).(map[string]any)
	items[1]["name"] = "mutated"
	value["bytes"].([]byte)[0] = 'z'

	clonedItems := cloned["items"].([]map[string]any)
	if clonedItems[0] != nil {
		t.Fatalf("expected nil item to be preserved")
	}
	if got := clonedItems[1]["name"]; got != "original" {
		t.Fatalf("expected cloned nested item to stay original, got %v", got)
	}
	if got := string(cloned["bytes"].([]byte)); got != "abc" {
		t.Fatalf("expected cloned bytes to stay abc, got %q", got)
	}
}

type customMessageWithoutClone struct {
	Message
}

type immutableCustomMessage struct {
	Message
}

func (m *immutableCustomMessage) ImmutableMessageEnvelope() {}

type cloneableCustomMessage struct {
	Message
	extra string
}

func (m *cloneableCustomMessage) CloneMessageEnvelope() IMessage {
	clone := *m
	clone.Message = CloneMessage(m.Message)
	return &clone
}

func TestCloneMessageEnvelopeRejectsCustomMessageWithoutExplicitContract(t *testing.T) {
	_, err := CloneMessageEnvelope(&customMessageWithoutClone{
		Message: Message{ID: "m1", Type: "custom", Timestamp: time.Now()},
	})
	if err == nil {
		t.Fatal("expected custom message without clone contract to fail")
	}
}

func TestCloneMessageEnvelopeKeepsCloneableConcreteType(t *testing.T) {
	original := &cloneableCustomMessage{
		Message: Message{
			ID:       "m1",
			Type:     "custom",
			Payload:  NewPayload(map[string]any{"name": "original"}),
			Metadata: NewMetadata(),
		},
		extra: "kept",
	}
	original.Metadata.Set("trace_id", "t1")

	cloned, err := CloneMessageEnvelope(original)
	if err != nil {
		t.Fatalf("CloneMessageEnvelope returned error: %v", err)
	}
	typed, ok := cloned.(*cloneableCustomMessage)
	if !ok {
		t.Fatalf("expected concrete clone type to be preserved, got %T", cloned)
	}
	PayloadValue(original.Payload).(map[string]any)["name"] = "mutated"
	original.Metadata.Set("trace_id", "mutated")

	if typed.extra != "kept" {
		t.Fatalf("expected extra field to be preserved, got %q", typed.extra)
	}
	if got := PayloadValue(typed.GetPayload()).(map[string]any)["name"]; got != "original" {
		t.Fatalf("expected payload clone to stay original, got %v", got)
	}
	if got, _ := typed.GetMetadata().GetString("trace_id"); got != "t1" {
		t.Fatalf("expected metadata clone to stay t1, got %q", got)
	}
}

func TestCloneMessageEnvelopeAllowsExplicitImmutableCustomMessage(t *testing.T) {
	original := &immutableCustomMessage{
		Message: Message{ID: "m1", Type: "custom", Timestamp: time.Now()},
	}
	cloned, err := CloneMessageEnvelope(original)
	if err != nil {
		t.Fatalf("CloneMessageEnvelope immutable returned error: %v", err)
	}
	if cloned != original {
		t.Fatal("expected immutable message to reuse original instance")
	}
}
