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

func TestMemoryLockProvider_AcquireRejectsAlreadyCanceledContext(t *testing.T) {
	p := NewMemoryLockProvider()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Acquire(ctx, "k")
	if err == nil {
		t.Fatalf("expected context error")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestMemoryLockProvider_ReleasesIdleKeys(t *testing.T) {
	p := NewMemoryLockProvider()

	release, err := p.Acquire(context.Background(), "k")
	if err != nil {
		t.Fatalf("Acquire error: %v", err)
	}
	release()
	release()

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.locks) != 0 {
		t.Fatalf("expected idle lock key to be removed, got %d", len(p.locks))
	}
}

func TestMemoryLockProvider_AcquireLeaseLostClosesOnRelease(t *testing.T) {
	p := NewMemoryLockProvider()

	lease, err := p.AcquireLease(context.Background(), "k")
	if err != nil {
		t.Fatalf("AcquireLease error: %v", err)
	}

	lease.Release()

	select {
	case lostErr, ok := <-lease.Lost():
		if ok {
			t.Fatalf("expected lost channel to close without value, got %v", lostErr)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected lost channel to close after release")
	}
}

func TestMemoryLockProvider_AcquireLeaseLostClosesForNilLease(t *testing.T) {
	var lease *memoryLease

	select {
	case _, ok := <-lease.Lost():
		if ok {
			t.Fatal("expected nil lease lost channel to be closed")
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("expected nil lease lost channel to close immediately")
	}
}

func TestMemoryLockProvider_AcquireLeaseReleaseIsIdempotent(t *testing.T) {
	p := NewMemoryLockProvider()

	lease, err := p.AcquireLease(context.Background(), "k")
	if err != nil {
		t.Fatalf("AcquireLease error: %v", err)
	}

	lease.Release()
	lease.Release()

	release, err := p.Acquire(context.Background(), "k")
	if err != nil {
		t.Fatalf("Acquire after double release error: %v", err)
	}
	release()
}
