package messaging

import "testing"

type payloadTestValue struct{}

func TestPayloadIsNil_DetectsTypedNil(t *testing.T) {
	var ptr *payloadTestValue
	if !NewPayload(ptr).IsNil() {
		t.Fatal("expected typed nil payload to be treated as nil")
	}
}
