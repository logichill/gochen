package operation

import (
	"context"
	"fmt"
	"testing"
)

func TestMemoryStoreMaxRecordsEvictsOldest(t *testing.T) {
	store := NewMemoryStore(WithMaxRecords(2))
	ctx := context.Background()
	put := func(id string) {
		if err := store.Put(ctx, &Result{Operation: Operation{ID: id, Type: "task.create", Mode: ModeTracked, Status: StatusSettled}}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	put("op-1")
	put("op-2")
	put("op-3")

	// op-1 最旧，应被淘汰；op-2/op-3 保留。
	if _, err := store.Get(ctx, "op-1"); err == nil {
		t.Fatalf("expected op-1 evicted")
	}
	for _, id := range []string{"op-2", "op-3"} {
		if _, err := store.Get(ctx, id); err != nil {
			t.Fatalf("expected %s retained: %v", id, err)
		}
	}
	if len(store.records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(store.records))
	}
}

func TestMemoryStoreMaxRecordsEvictionUnbindsIdempotencyKey(t *testing.T) {
	store := NewMemoryStore(WithMaxRecords(1))
	ctx := context.Background()
	first := &Result{Operation: Operation{ID: "op-1", Type: "task.create", Mode: ModeTracked, Status: StatusSettled}}
	if err := store.PutWithIdempotencyKey(ctx, "key-1", first); err != nil {
		t.Fatalf("put first: %v", err)
	}
	second := &Result{Operation: Operation{ID: "op-2", Type: "task.create", Mode: ModeTracked, Status: StatusSettled}}
	if err := store.PutWithIdempotencyKey(ctx, "key-2", second); err != nil {
		t.Fatalf("put second: %v", err)
	}

	// op-1 被淘汰后，key-1 绑定同步解除，同键再写视为首次执行。
	if _, err := store.GetByIdempotencyKey(ctx, "key-1"); err == nil {
		t.Fatalf("expected key-1 unbound after eviction")
	}
	third := &Result{Operation: Operation{ID: "op-3", Type: "task.create", Mode: ModeTracked, Status: StatusSettled}}
	if err := store.PutWithIdempotencyKey(ctx, "key-1", third); err != nil {
		t.Fatalf("put with reused key after eviction: %v", err)
	}
}

func TestMemoryStoreMaxRecordsKeepsReservationAndTombstone(t *testing.T) {
	store := NewMemoryStore(WithMaxRecords(1))
	ctx := context.Background()
	if err := store.ReserveIdempotencyKey(ctx, "key-r", "op-reserved"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := store.Put(ctx, &Result{Operation: Operation{ID: "op-done", Type: "task.create", Mode: ModeTracked, Status: StatusSettled}}); err != nil {
		t.Fatalf("put done: %v", err)
	}

	// 回读断言：Put 报成功后 Get 必须能读到刚写入的记录——
	// 预占占位挤占容量时，绝不淘汰本调用刚写入的记录。
	if _, err := store.Get(ctx, "op-done"); err != nil {
		t.Fatalf("just-written record must survive its own Put: %v", err)
	}

	// 预占中的记录不淘汰：Get 对预占记录按设计返回 NotFound（对外不可见），
	// 这里直接验证内部记录未被淘汰且预占标记仍在。
	store.mu.RLock()
	_, reservedExists := store.records["op-reserved"]
	_, reservedMark := store.idempotencyReservations["op-reserved"]
	store.mu.RUnlock()
	if !reservedExists || !reservedMark {
		t.Fatalf("expected reserved record retained with reservation mark")
	}

	// tombstone 拦截：删除后同 ID 复活仍被拒绝。
	if err := store.Delete(ctx, "op-done"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.Put(ctx, &Result{Operation: Operation{ID: "op-done", Type: "task.create", Mode: ModeTracked, Status: StatusSettled}}); err == nil {
		t.Fatalf("expected tombstone to block resurrect")
	}
}

func TestMemoryStoreUnlimitedByDefault(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		id := "op-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		if err := store.Put(ctx, &Result{Operation: Operation{ID: id, Type: "task.create", Mode: ModeTracked, Status: StatusSettled}}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	if len(store.records) != 100 {
		t.Fatalf("expected 100 records without cap, got %d", len(store.records))
	}
}

// 重复 Put 覆盖不刷新 FIFO 顺序：淘汰基准仍是首次写入顺序。
func TestMemoryStoreOverwriteKeepsFIFOOrder(t *testing.T) {
	store := NewMemoryStore(WithMaxRecords(2))
	ctx := context.Background()
	put := func(id string) {
		if err := store.Put(ctx, &Result{Operation: Operation{ID: id, Type: "task.create", Mode: ModeTracked, Status: StatusSettled}}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	put("op-1")
	put("op-2")
	put("op-1") // 覆盖，不改变顺序
	put("op-3")

	if _, err := store.Get(ctx, "op-1"); err == nil {
		t.Fatalf("expected op-1 evicted (original FIFO position)")
	}
	for _, id := range []string{"op-2", "op-3"} {
		if _, err := store.Get(ctx, id); err != nil {
			t.Fatalf("expected %s retained: %v", id, err)
		}
	}
}

// 低于容量运行时（淘汰永不触发）下，put/delete 流水不得让
// recordOrder 无界累积历史 id：Delete 即时清理顺序项。
func TestMemoryStoreRecordOrderBoundedUnderBelowCapacityChurn(t *testing.T) {
	store := NewMemoryStore(WithMaxRecords(10))
	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("op-%d", i)
		if err := store.Put(ctx, &Result{Operation: Operation{ID: id, Type: "task.create", Mode: ModeTracked, Status: StatusSettled}}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
		if err := store.Delete(ctx, id); err != nil {
			t.Fatalf("delete %s: %v", id, err)
		}
	}
	if len(store.records) != 0 {
		t.Fatalf("expected no records left, got %d", len(store.records))
	}
	if len(store.recordOrderIndex) != 0 {
		t.Fatalf("expected empty record order index, got %d", len(store.recordOrderIndex))
	}
	if len(store.recordOrder) > 128 {
		t.Fatalf("record order must stay bounded under churn, got %d entries", len(store.recordOrder))
	}
}
