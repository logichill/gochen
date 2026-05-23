package operation

import (
	"context"
	"testing"
)

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

func TestMemoryStoreGetDeepClonesNestedPayloads(t *testing.T) {
	store := NewMemoryStore()
	err := store.Put(context.Background(), &Result{
		Operation: Operation{ID: "op_nested"},
		Result: map[string]any{
			"nested": map[string]any{"x": 1},
			"items":  []any{map[string]any{"y": 2}},
		},
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.Get(context.Background(), "op_nested")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got.Result["nested"].(map[string]any)["x"] = 9
	got.Result["items"].([]any)[0].(map[string]any)["y"] = 8

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
}
