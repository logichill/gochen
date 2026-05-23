package httpx

import (
	"context"
	"testing"

	"gochen/contextx"
)

func TestRequestContextWithContextNilFallsBackToTraceableContext(t *testing.T) {
	reqCtx, err := NewRequestContext(context.Background())
	if err != nil {
		t.Fatalf("NewRequestContext() error = %v", err)
	}

	derived := reqCtx.WithContext(nil)
	if derived == nil {
		t.Fatal("WithContext(nil) returned nil")
	}
	if got := contextx.TraceID(derived); got == "" {
		t.Fatal("WithContext(nil) should fall back to a traceable context")
	}
}
