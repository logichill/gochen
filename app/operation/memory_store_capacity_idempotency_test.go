package operation

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// 容量 × 幂等交错回归（P1-1 / f430e68e）。
//
// 根因：预占占位记录计入 len(records) 但不进入 FIFO 顺序表；
// 若淘汰试图用顺序表回收全部溢出，最后被淘汰的往往是本次刚写入的记录，
// 且 PutWithIdempotencyKey 会把幂等键绑定到已删除记录（幽灵绑定），
// 同键重放因 GetByIdempotencyKey 返回 NotFound 而重新执行业务回调。
// 修复语义：本调用刚写入/补存的记录永不参与本次淘汰；溢出无法回收时
// 宁可暂时超容量。以下测试锁住"Put 报成功 ⇒ Get 可读 ⇒ 同键重放命中"链路。

func settledResult(id string) *Result {
	return &Result{Operation: Operation{ID: id, Type: "task.create", Mode: ModeTracked, Status: StatusSettled}}
}

// 预占占满容量后，PutWithIdempotencyKey 报成功必须可读且绑定有效
// （绑定有效 ⇒ runner 重放同键不会重新执行业务回调）。
func TestMemoryStoreCapacityReservationKeepsIdempotentWriteReadable(t *testing.T) {
	store := NewMemoryStore(WithMaxRecords(1))
	ctx := context.Background()
	if err := store.ReserveIdempotencyKey(ctx, "key-r", "op-reserved"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := store.PutWithIdempotencyKey(ctx, "key-done", settledResult("op-done")); err != nil {
		t.Fatalf("put with idempotency key: %v", err)
	}

	got, err := store.Get(ctx, "op-done")
	if err != nil {
		t.Fatalf("just-written record must be readable after Put: %v", err)
	}
	if got.Operation.ID != "op-done" {
		t.Fatalf("expected op-done, got %q", got.Operation.ID)
	}
	bound, err := store.GetByIdempotencyKey(ctx, "key-done")
	if err != nil {
		t.Fatalf("idempotency key must not become a ghost binding: %v", err)
	}
	if bound.Operation.ID != "op-done" {
		t.Fatalf("expected key-done bound to op-done, got %q", bound.Operation.ID)
	}
}

// 容量 × 幂等全交错：预占占满 → 带键写入保位可读 → 释放预占后容量恢复 →
// FIFO 淘汰继续解除被淘汰记录的键绑定。
func TestMemoryStoreCapacityIdempotencyInterleaving(t *testing.T) {
	store := NewMemoryStore(WithMaxRecords(2))
	ctx := context.Background()
	for _, r := range []struct{ key, op string }{
		{"key-a", "op-a"},
		{"key-b", "op-b"},
	} {
		if err := store.ReserveIdempotencyKey(ctx, r.key, r.op); err != nil {
			t.Fatalf("reserve %s: %v", r.key, err)
		}
	}

	// 预占占满容量：带键写入仍必须成功且可读、绑定有效。
	if err := store.PutWithIdempotencyKey(ctx, "key-c", settledResult("op-c")); err != nil {
		t.Fatalf("put op-c under full reservation: %v", err)
	}
	if _, err := store.Get(ctx, "op-c"); err != nil {
		t.Fatalf("op-c must survive its own write: %v", err)
	}
	if bound, err := store.GetByIdempotencyKey(ctx, "key-c"); err != nil || bound.Operation.ID != "op-c" {
		t.Fatalf("key-c must resolve to op-c after write (err=%v)", err)
	}

	// 释放一个预占后容量恢复：下一次写入触发 FIFO 淘汰（淘汰最旧的 op-c）。
	if err := store.ReleaseIdempotencyKey(ctx, "key-a", "op-a"); err != nil {
		t.Fatalf("release key-a: %v", err)
	}
	if err := store.PutWithIdempotencyKey(ctx, "key-e", settledResult("op-e")); err != nil {
		t.Fatalf("put op-e: %v", err)
	}
	if _, err := store.Get(ctx, "op-c"); err == nil {
		t.Fatalf("expected op-c evicted as oldest")
	}
	if _, err := store.GetByIdempotencyKey(ctx, "key-c"); err == nil {
		t.Fatalf("expected key-c unbound after op-c eviction")
	}
	if _, err := store.Get(ctx, "op-e"); err != nil {
		t.Fatalf("expected op-e retained: %v", err)
	}
}

// RecoverResult 补存的新记录与 Put 同等受容量约束：
// 登记 FIFO 顺序、可被后续写入淘汰、淘汰同步解除键绑定。
func TestMemoryStoreRecoverResultSubjectToCapacity(t *testing.T) {
	store := NewMemoryStore(WithMaxRecords(1))
	ctx := context.Background()
	recovered := &Result{Operation: Operation{ID: "op-rec", Type: "task.create", Mode: ModeTracked, Status: StatusSettled}}
	got, err := store.RecoverResult(ctx, "key-rec", recovered)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if got.Operation.ID != "op-rec" {
		t.Fatalf("expected op-rec, got %q", got.Operation.ID)
	}

	// 补存记录应占满容量：下一条写入把它淘汰并解除 key-rec 绑定。
	if err := store.Put(ctx, settledResult("op-next")); err != nil {
		t.Fatalf("put op-next: %v", err)
	}
	if _, err := store.Get(ctx, "op-rec"); err == nil {
		t.Fatalf("expected recovered record evicted under capacity")
	}
	if _, err := store.GetByIdempotencyKey(ctx, "key-rec"); err == nil {
		t.Fatalf("expected key-rec unbound after eviction")
	}
	if _, err := store.Get(ctx, "op-next"); err != nil {
		t.Fatalf("expected op-next retained: %v", err)
	}
}

// 并发容量流水：多 goroutine 交错 put/delete/带键写入下，
// 每次成功写入的记录在写入返回后立即可读（不允许幽灵绑定/静默丢失）。
func TestMemoryStoreConcurrentCapacityChurnKeepsWritesReadable(t *testing.T) {
	store := NewMemoryStore(WithMaxRecords(8))
	ctx := context.Background()

	const workers = 8
	const iterations = 200
	var wg sync.WaitGroup
	errCh := make(chan error, workers*2)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				id := fmt.Sprintf("op-%d-%d", w, i)
				key := fmt.Sprintf("key-%d-%d", w, i)
				if err := store.PutWithIdempotencyKey(ctx, key, settledResult(id)); err != nil {
					errCh <- fmt.Errorf("put %s: %w", id, err)
					return
				}
				if _, err := store.Get(ctx, id); err != nil {
					// 容量为 8 且写入远快于淘汰节奏时，本条记录可能已被
					// 后续并发写入按 FIFO 淘汰；但绝不允许"从未写入成功"
					// ——用幂等键回读复核，绑定必须与记录同生同灭。
					if _, kerr := store.GetByIdempotencyKey(ctx, key); kerr == nil {
						errCh <- fmt.Errorf("key %s still bound while record %s unreadable", key, id)
						return
					}
				}
				if err := store.Delete(ctx, id); err != nil {
					errCh <- fmt.Errorf("delete %s: %w", id, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}
