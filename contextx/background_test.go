package contextx

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBackground_HasTraceID(t *testing.T) {
	ctx := Background()
	if ctx == nil {
		t.Fatalf("expected non-nil context")
	}
	if got := TraceID(ctx); got == "" {
		t.Fatalf("expected non-empty trace_id")
	} else if !strings.HasPrefix(got, "trc-") {
		t.Fatalf("expected trc- prefix, got %q", got)
	}
}

func TestGenerateTraceIDFallsBackWhenUUIDFails(t *testing.T) {
	now := func() time.Time { return time.Unix(0, 123) }
	failingUUID := func() (string, error) { return "", errors.New("uuid unavailable") }

	if got := generateTraceID(failingUUID, now); got != "trc-123" {
		t.Fatalf("unexpected fallback trace ID: %q", got)
	}
}

func TestGenerateTraceIDReturnsUniqueUUIDs(t *testing.T) {
	first := GenerateTraceID()
	second := GenerateTraceID()
	if first == second {
		t.Fatalf("expected unique trace IDs, got %q", first)
	}
	for _, traceID := range []string{first, second} {
		if !strings.HasPrefix(traceID, "trc-") {
			t.Fatalf("expected trc- prefix, got %q", traceID)
		}
		if len(strings.TrimPrefix(traceID, "trc-")) != 36 {
			t.Fatalf("expected UUID trace_id, got %q", traceID)
		}
	}
}
