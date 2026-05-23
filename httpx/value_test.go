package httpx

import "testing"

type valueTestPayload struct{}

func TestJSONBodyIsNil_DetectsTypedNil(t *testing.T) {
	var ptr *valueTestPayload
	if !JSONValue(ptr).IsNil() {
		t.Fatal("expected typed nil JSON body to be treated as nil")
	}
}

func TestContextValueIsNil_DetectsTypedNil(t *testing.T) {
	var ptr *valueTestPayload
	if !ValueOf(ptr).IsNil() {
		t.Fatal("expected typed nil context value to be treated as nil")
	}
}
