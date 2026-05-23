package snapshot

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore_DefensivelyCopiesSnapshot(t *testing.T) {
	store := NewMemoryStore[int64]()
	data := []byte(`{"value":1}`)
	metadata := map[string]any{
		"source": "seed",
		"nested": map[string]any{"scope": "tenant-a"},
		"rules":  []any{"rule-a", map[string]any{"kind": "doc"}},
		"tags":   []string{"alpha"},
		"bytes":  []byte("abc"),
	}
	err := store.SaveSnapshot(context.Background(), Snapshot[int64]{
		AggregateID:   1,
		AggregateType: "test",
		Version:       1,
		Data:          data,
		Timestamp:     time.Now(),
		Metadata:      metadata,
	})
	if err != nil {
		t.Fatalf("save snapshot failed: %v", err)
	}
	data[9] = '9'
	metadata["source"] = "mutated-before-load"
	metadata["nested"].(map[string]any)["scope"] = "mutated-before-load"
	metadata["rules"].([]any)[1].(map[string]any)["kind"] = "mutated-before-load"
	metadata["tags"].([]string)[0] = "mutated-before-load"
	metadata["bytes"].([]byte)[0] = 'x'

	loaded, err := store.FindSnapshot(context.Background(), "test", 1)
	if err != nil {
		t.Fatalf("find snapshot failed: %v", err)
	}
	if string(loaded.Data) != `{"value":1}` {
		t.Fatalf("expected stored data to be isolated, got %s", string(loaded.Data))
	}
	if loaded.Metadata["source"] != "seed" {
		t.Fatalf("expected stored metadata to be isolated, got %v", loaded.Metadata["source"])
	}
	if loaded.Metadata["nested"].(map[string]any)["scope"] != "tenant-a" {
		t.Fatalf("expected nested metadata to be isolated, got %v", loaded.Metadata["nested"])
	}
	if loaded.Metadata["rules"].([]any)[1].(map[string]any)["kind"] != "doc" {
		t.Fatalf("expected nested list metadata to be isolated, got %v", loaded.Metadata["rules"])
	}
	if loaded.Metadata["tags"].([]string)[0] != "alpha" {
		t.Fatalf("expected string slice metadata to be isolated, got %v", loaded.Metadata["tags"])
	}
	if string(loaded.Metadata["bytes"].([]byte)) != "abc" {
		t.Fatalf("expected byte slice metadata to be isolated, got %v", loaded.Metadata["bytes"])
	}
	loaded.Data[9] = '8'
	loaded.Metadata["source"] = "mutated-after-find"
	loaded.Metadata["nested"].(map[string]any)["scope"] = "mutated-after-find"
	loaded.Metadata["rules"].([]any)[1].(map[string]any)["kind"] = "mutated-after-find"
	loaded.Metadata["tags"].([]string)[0] = "mutated-after-find"
	loaded.Metadata["bytes"].([]byte)[0] = 'y'

	list, err := store.ListSnapshots(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("list snapshots failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(list))
	}
	if string(list[0].Data) != `{"value":1}` {
		t.Fatalf("expected listed data to be isolated, got %s", string(list[0].Data))
	}
	list[0].Data[9] = '7'
	list[0].Metadata["source"] = "mutated-after-list"
	list[0].Metadata["nested"].(map[string]any)["scope"] = "mutated-after-list"
	list[0].Metadata["rules"].([]any)[1].(map[string]any)["kind"] = "mutated-after-list"
	list[0].Metadata["tags"].([]string)[0] = "mutated-after-list"
	list[0].Metadata["bytes"].([]byte)[0] = 'z'

	loadedAgain, err := store.FindSnapshot(context.Background(), "test", 1)
	if err != nil {
		t.Fatalf("find snapshot again failed: %v", err)
	}
	if string(loadedAgain.Data) != `{"value":1}` {
		t.Fatalf("expected data to remain isolated, got %s", string(loadedAgain.Data))
	}
	if loadedAgain.Metadata["source"] != "seed" {
		t.Fatalf("expected metadata to remain isolated, got %v", loadedAgain.Metadata["source"])
	}
	if loadedAgain.Metadata["nested"].(map[string]any)["scope"] != "tenant-a" {
		t.Fatalf("expected nested metadata to remain isolated, got %v", loadedAgain.Metadata["nested"])
	}
	if loadedAgain.Metadata["rules"].([]any)[1].(map[string]any)["kind"] != "doc" {
		t.Fatalf("expected nested list metadata to remain isolated, got %v", loadedAgain.Metadata["rules"])
	}
	if loadedAgain.Metadata["tags"].([]string)[0] != "alpha" {
		t.Fatalf("expected string slice metadata to remain isolated, got %v", loadedAgain.Metadata["tags"])
	}
	if string(loadedAgain.Metadata["bytes"].([]byte)) != "abc" {
		t.Fatalf("expected byte slice metadata to remain isolated, got %v", loadedAgain.Metadata["bytes"])
	}
}

func TestMemoryStore_CleanupSnapshotsNonPositiveRetentionNoop(t *testing.T) {
	store := NewMemoryStore[int64]()
	err := store.SaveSnapshot(context.Background(), Snapshot[int64]{
		AggregateID:   1,
		AggregateType: "test",
		Version:       1,
		Data:          []byte(`{}`),
		Timestamp:     time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("save snapshot failed: %v", err)
	}
	if err := store.CleanupSnapshots(context.Background(), 0); err != nil {
		t.Fatalf("cleanup snapshots failed: %v", err)
	}
	if _, err := store.FindSnapshot(context.Background(), "test", 1); err != nil {
		t.Fatalf("expected snapshot to survive zero retention cleanup: %v", err)
	}
}
