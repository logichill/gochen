package operation

import (
	"context"
	"testing"
)

func TestMemoryStorePutWithIdempotencyKeyRebindsMissingBoundOperation(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	first := &Result{Operation: Operation{ID: "op_first", Type: "task.create", Mode: ModeTracked, Status: StatusAccepted}}
	if err := store.PutWithIdempotencyKey(ctx, "request-1", first); err != nil {
		t.Fatalf("put first: %v", err)
	}
	store.mu.Lock()
	delete(store.records, "op_first")
	store.mu.Unlock()

	second := &Result{Operation: Operation{ID: "op_second", Type: "task.create", Mode: ModeTracked, Status: StatusAccepted}}
	if err := store.PutWithIdempotencyKey(ctx, "request-1", second); err != nil {
		t.Fatalf("put second after missing binding: %v", err)
	}
	got, err := store.GetByIdempotencyKey(ctx, "request-1")
	if err != nil {
		t.Fatalf("get rebound key: %v", err)
	}
	if got.Operation.ID != "op_second" {
		t.Fatalf("expected rebound operation, got %q", got.Operation.ID)
	}
}
