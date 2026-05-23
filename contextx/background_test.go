package contextx

import "testing"

func TestBackground_HasTraceID(t *testing.T) {
	ctx := Background()
	if ctx == nil {
		t.Fatalf("expected non-nil context")
	}
	if got := TraceID(ctx); got == "" {
		t.Fatalf("expected non-empty trace_id")
	}
}
