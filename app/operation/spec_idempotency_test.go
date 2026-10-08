package operation

import (
	"strings"
	"testing"
)

func TestSpecValidateRejectsInvalidIdempotencyKey(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{name: "leading space", key: " request-1"},
		{name: "embedded space", key: "request 1"},
		{name: "control", key: "request\n1"},
		{name: "too long", key: strings.Repeat("a", maxIdempotencyKeyLength+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (&Spec{Type: "task.complete", Mode: ModeTracked, IdempotencyKey: tc.key}).Validate()
			if err == nil {
				t.Fatalf("expected invalid idempotency key to be rejected")
			}
		})
	}
}

func TestSpecValidateAcceptsBoundedIdempotencyKey(t *testing.T) {
	err := (&Spec{Type: "task.complete", Mode: ModeTracked, IdempotencyKey: strings.Repeat("a", maxIdempotencyKeyLength)}).Validate()
	if err != nil {
		t.Fatalf("expected max-length idempotency key to be accepted: %v", err)
	}
}
