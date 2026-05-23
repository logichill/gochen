package lock

import (
	"context"
	"testing"
	"time"
)

// TestMemoryLockProvider_SerializesByKey 验证 MemoryLockProvider SerializesByKey。
func TestMemoryLockProvider_SerializesByKey(t *testing.T) {
	p := NewMemoryLockProvider()

	ctx := context.Background()
	release1, err := p.Acquire(ctx, "k")
	if err != nil {
		t.Fatalf("Acquire error: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		defer close(acquired)
		r2, err := p.Acquire(ctx, "k")
		if err != nil {
			t.Errorf("Acquire error: %v", err)
			return
		}
		defer r2()
	}()

	select {
	case <-acquired:
		t.Fatalf("expected second acquire to block until release")
	case <-time.After(30 * time.Millisecond):
	}

	release1()
	select {
	case <-acquired:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected acquire to proceed after release")
	}
}

// TestMemoryLockProvider_EmptyKey 验证 MemoryLockProvider EmptyKey。
func TestMemoryLockProvider_EmptyKey(t *testing.T) {
	p := NewMemoryLockProvider()
	_, err := p.Acquire(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error")
	}
}

// TestMemoryLockProvider_AcquireRespectsContextCancellation 验证 MemoryLockProvider AcquireRespectsContextCancellation。
func TestMemoryLockProvider_AcquireRespectsContextCancellation(t *testing.T) {
	p := NewMemoryLockProvider()

	ctx := context.Background()
	release, err := p.Acquire(ctx, "k")
	if err != nil {
		t.Fatalf("Acquire error: %v", err)
	}
	defer release()

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = p.Acquire(timeoutCtx, "k")
	if err == nil {
		t.Fatalf("expected context error")
	}
}
