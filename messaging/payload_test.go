package messaging

import (
	"testing"

	"gochen/errors"
)

type payloadTestValue struct{}

func TestPayloadIsNil_DetectsTypedNil(t *testing.T) {
	var ptr *payloadTestValue
	if !NewPayload(ptr).IsNil() {
		t.Fatal("expected typed nil payload to be treated as nil")
	}
}

func TestPayloadDecodeToNilTargetIsInvalidInput(t *testing.T) {
	err := NewPayload(map[string]any{}).DecodeTo(nil)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

func TestPayloadUnmarshalJSONNilReceiverIsInvalidInput(t *testing.T) {
	var payload *Payload
	err := payload.UnmarshalJSON([]byte(`{}`))
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}
