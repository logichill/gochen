package operation

import (
	"context"
	"strconv"
	"testing"

	"gochen/errors"
)

func TestMemoryStoreZeroValueSupportsWrites(t *testing.T) {
	var store MemoryStore
	ctx := context.Background()
	result := &Result{Operation: Operation{ID: "op_zero", Mode: ModeTracked, Status: StatusAccepted}}

	if err := store.PutWithIdempotencyKey(ctx, "request-zero", result); err != nil {
		t.Fatalf("put zero-value store: %v", err)
	}
	got, err := store.GetByIdempotencyKey(ctx, "request-zero")
	if err != nil {
		t.Fatalf("get zero-value store: %v", err)
	}
	if got.Operation.ID != result.Operation.ID {
		t.Fatalf("operation id = %q, want %q", got.Operation.ID, result.Operation.ID)
	}

	var deletedStore MemoryStore
	if err := deletedStore.Delete(ctx, "op_deleted"); err != nil {
		t.Fatalf("delete from zero-value store: %v", err)
	}
	if err := deletedStore.Put(ctx, &Result{Operation: Operation{ID: "op_deleted"}}); !errors.Is(err, errors.Conflict) {
		t.Fatalf("put deleted operation error = %v, want conflict", err)
	}
}

func TestMemoryStoreHonorsContextAcrossPublicOperations(t *testing.T) {
	store := NewMemoryStore()
	result := &Result{Operation: Operation{ID: "op_ctx", Mode: ModeTracked, Status: StatusAccepted}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		call func(context.Context) error
	}{
		{
			name: "get",
			call: func(ctx context.Context) error {
				_, err := store.Get(ctx, result.Operation.ID)
				return err
			},
		},
		{
			name: "get by idempotency key",
			call: func(ctx context.Context) error {
				_, err := store.GetByIdempotencyKey(ctx, "request-ctx")
				return err
			},
		},
		{name: "put", call: func(ctx context.Context) error { return store.Put(ctx, result) }},
		{
			name: "put with idempotency key",
			call: func(ctx context.Context) error {
				return store.PutWithIdempotencyKey(ctx, "request-ctx", result)
			},
		},
		{
			name: "reserve idempotency key",
			call: func(ctx context.Context) error {
				return store.ReserveIdempotencyKey(ctx, "request-ctx", result.Operation.ID)
			},
		},
		{
			name: "release idempotency key",
			call: func(ctx context.Context) error {
				return store.ReleaseIdempotencyKey(ctx, "request-ctx", result.Operation.ID)
			},
		},
		{name: "delete", call: func(ctx context.Context) error { return store.Delete(ctx, result.Operation.ID) }},
	}

	for _, test := range tests {
		t.Run(test.name+" canceled", func(t *testing.T) {
			if err := test.call(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
		})
		t.Run(test.name+" nil", func(t *testing.T) {
			if err := test.call(nil); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("error = %v, want invalid input", err)
			}
		})
	}
}

func TestMemoryStorePutAndGet(t *testing.T) {
	store := NewMemoryStore()
	err := store.Put(context.Background(), &Result{
		Operation: Operation{
			ID:     "op_1",
			Type:   "task.create",
			Mode:   ModeTracked,
			Status: StatusAccepted,
		},
		AffectedScopes: []string{"tasks:list"},
		Result:         map[string]any{"task_id": 1},
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.Get(context.Background(), "op_1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Operation.Status != StatusAccepted {
		t.Fatalf("unexpected status: %s", got.Operation.Status)
	}
	got.AffectedScopes[0] = "changed"

	reloaded, err := store.Get(context.Background(), "op_1")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.AffectedScopes[0] != "tasks:list" {
		t.Fatalf("store should return clone, got %+v", reloaded.AffectedScopes)
	}
}

func TestIsStoreNotFoundRecognizesCodedNotFound(t *testing.T) {
	if !IsStoreNotFound(errors.NewCode(errors.NotFound, "missing")) {
		t.Fatal("expected errors.NotFound to be recognized as store not found")
	}
	if IsStoreNotFound(errors.NewCode(errors.Database, "db failed")) {
		t.Fatal("database errors must not be recognized as store not found")
	}
}

func TestMemoryStoreGetDeepClonesNestedPayloads(t *testing.T) {
	store := NewMemoryStore()
	data := []byte("hello")
	tags := []string{"a", "b"}
	err := store.Put(context.Background(), &Result{
		Operation: Operation{ID: "op_nested"},
		Result: map[string]any{
			"nested": map[string]any{"x": 1},
			"items":  []any{map[string]any{"y": 2}},
			"data":   data,
			"tags":   tags,
		},
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	data[0] = 'X'
	tags[0] = "changed"

	got, err := store.Get(context.Background(), "op_nested")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got.Result["nested"].(map[string]any)["x"] = 9
	got.Result["items"].([]any)[0].(map[string]any)["y"] = 8
	got.Result["data"].([]byte)[0] = 'Y'
	got.Result["tags"].([]string)[0] = "mutated"

	reloaded, err := store.Get(context.Background(), "op_nested")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Result["nested"].(map[string]any)["x"] != 1 {
		t.Fatalf("expected nested map to stay isolated, got %+v", reloaded.Result["nested"])
	}
	if reloaded.Result["items"].([]any)[0].(map[string]any)["y"] != 2 {
		t.Fatalf("expected nested slice item to stay isolated, got %+v", reloaded.Result["items"])
	}
	if string(reloaded.Result["data"].([]byte)) != "hello" {
		t.Fatalf("expected []byte to stay isolated, got %q", string(reloaded.Result["data"].([]byte)))
	}
	if reloaded.Result["tags"].([]string)[0] != "a" {
		t.Fatalf("expected []string to stay isolated, got %+v", reloaded.Result["tags"])
	}
}

func TestMemoryStore_PurgeDeletedRemovesTombstones(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	if err := store.Delete(ctx, "op_deleted"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := store.PurgeDeleted("op_deleted"); got != 1 {
		t.Fatalf("expected one purged tombstone, got %d", got)
	}
	if err := store.PutWithIdempotencyKey(ctx, "key-delete", &Result{Operation: Operation{ID: "op_deleted"}}); err != nil {
		t.Fatalf("put after purge: %v", err)
	}
	if got := store.PurgeDeleted(); got != 0 {
		t.Fatalf("expected no remaining tombstones, got %d", got)
	}
}

func TestMemoryStore_PutWithIdempotencyKeyKeepsOtherReservation(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	if err := store.ReserveIdempotencyKey(ctx, "request-1", "op_owner"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	store.mu.RLock()
	reserved := store.records["op_owner"]
	_, reservationMarked := store.idempotencyReservations["op_owner"]
	store.mu.RUnlock()
	if !reservationMarked {
		t.Fatal("expected reservation marker")
	}
	if reserved.Operation.Type != "" {
		t.Fatalf("reservation should not use a public operation type, got %q", reserved.Operation.Type)
	}
	err := store.PutWithIdempotencyKey(ctx, "request-1", &Result{
		Operation: Operation{
			ID:     "op_other",
			Type:   "task.create",
			Mode:   ModeTracked,
			Status: StatusAccepted,
		},
	})
	if !errors.Is(err, errors.Conflict) {
		t.Fatalf("expected conflict for different operation reservation, got %v", err)
	}

	err = store.PutWithIdempotencyKey(ctx, "request-1", &Result{
		Operation: Operation{
			ID:     "op_owner",
			Type:   "task.create",
			Mode:   ModeTracked,
			Status: StatusAccepted,
		},
	})
	if err != nil {
		t.Fatalf("expected owner to finalize reservation: %v", err)
	}
	got, err := store.GetByIdempotencyKey(ctx, "request-1")
	if err != nil {
		t.Fatalf("get finalized result: %v", err)
	}
	if got.Operation.ID != "op_owner" {
		t.Fatalf("expected owner result, got %q", got.Operation.ID)
	}
}

func TestMemoryStore_ReleaseIdempotencyKeyOnlyRemovesReservation(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	if err := store.ReserveIdempotencyKey(ctx, "request-1", "op_reserved"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := store.ReleaseIdempotencyKey(ctx, "request-1", "op_reserved"); err != nil {
		t.Fatalf("release reservation: %v", err)
	}
	if _, err := store.GetByIdempotencyKey(ctx, "request-1"); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected reservation to be removed, got %v", err)
	}

	result := &Result{
		Operation: Operation{
			ID:     "op_done",
			Type:   "task.create",
			Mode:   ModeTracked,
			Status: StatusAccepted,
		},
	}
	if err := store.PutWithIdempotencyKey(ctx, "request-done", result); err != nil {
		t.Fatalf("put final result: %v", err)
	}
	if err := store.ReleaseIdempotencyKey(ctx, "request-done", "op_done"); err != nil {
		t.Fatalf("release final result should be no-op: %v", err)
	}
	got, err := store.GetByIdempotencyKey(ctx, "request-done")
	if err != nil {
		t.Fatalf("final result should remain: %v", err)
	}
	if got.Operation.ID != "op_done" {
		t.Fatalf("expected final result to remain, got %q", got.Operation.ID)
	}
}

func TestMemoryStore_PurgeDeletedWithoutArgsClearsAllTombstones(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	for _, id := range []string{"op_1", "op_2"} {
		if err := store.Delete(ctx, id); err != nil {
			t.Fatalf("delete %s: %v", id, err)
		}
	}

	if got := store.PurgeDeleted(); got != 2 {
		t.Fatalf("expected two purged tombstones, got %d", got)
	}
	if err := store.PutWithIdempotencyKey(ctx, "key-1", &Result{Operation: Operation{ID: "op_1"}}); err != nil {
		t.Fatalf("put after full purge op_1: %v", err)
	}
	if err := store.PutWithIdempotencyKey(ctx, "key-2", &Result{Operation: Operation{ID: "op_2"}}); err != nil {
		t.Fatalf("put after full purge op_2: %v", err)
	}
}

func TestMemoryStore_DeleteAutoPrunesTombstones(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	for i := 0; i < defaultMaxDeletedTombstones+10; i++ {
		if err := store.Delete(ctx, "op_"+strconv.Itoa(i)); err != nil {
			t.Fatalf("delete %d: %v", i, err)
		}
	}

	if got := len(store.deleted); got != defaultMaxDeletedTombstones {
		t.Fatalf("expected tombstones capped at %d, got %d", defaultMaxDeletedTombstones, got)
	}
	if _, ok := store.deleted["op_0"]; ok {
		t.Fatalf("expected oldest tombstone to be pruned")
	}
}
