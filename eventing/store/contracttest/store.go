// Package contracttest 提供事件存储实现可复用的行为契约测试。
// 每个 factory 必须创建空的、支持 int64 聚合 ID 的独立存储，并通过 t.Cleanup 释放资源。
package contracttest

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
)

// RunEventStore 验证聚合隔离、版本冲突、幂等、取消与追加原子性。
func RunEventStore(t *testing.T, factory func(*testing.T) store.IEventStore[int64]) {
	t.Helper()
	t.Run("aggregate_history", func(t *testing.T) {
		s := factory(t)
		appendEvents(t, s, "A", 1, 0, event("a1", "A", 1, 1), event("a2", "A", 1, 2))
		appendEvents(t, s, "B", 1, 0, event("b1", "B", 1, 1))
		got, err := s.LoadEvents(context.Background(), "A", 1, 1)
		must(t, err)
		if len(got) != 1 || got[0].ID != "a2" || got[0].Version != 2 {
			t.Fatalf("incremental history = %+v", got)
		}
		version(t, s, "A", 1, 2)
		version(t, s, "B", 1, 1)
		version(t, s, "A", 2, 0)
		for id, want := range map[int64]bool{1: true, 2: false} {
			exists, err := s.HasAggregate(context.Background(), "A", id)
			must(t, err)
			if exists != want {
				t.Fatalf("aggregate %d exists = %v", id, exists)
			}
		}
	})
	t.Run("optimistic_concurrency", func(t *testing.T) {
		s := factory(t)
		appendEvents(t, s, "A", 1, 0, event("first", "A", 1, 1))
		err := s.AppendEvents(context.Background(), "A", 1, []eventing.IStorableEvent[int64]{event("conflict", "A", 1, 1)}, 0)
		if !errors.Is(err, errors.Concurrency) {
			t.Fatalf("stale append = %v", err)
		}
		var detail *errors.AppError
		if !errors.As(err, &detail) {
			t.Fatalf("missing conflict details: %v", err)
		}
		values := detail.Details()
		if values["aggregate_id"] != int64(1) || values["expected_version"] != uint64(0) || values["actual_version"] != uint64(1) {
			t.Fatalf("conflict details = %v", values)
		}
		version(t, s, "A", 1, 1)
		appendEvents(t, s, "A", 1, 1, event("next", "A", 1, 2))
		version(t, s, "A", 1, 2)
	})
	t.Run("idempotent_retry", func(t *testing.T) {
		s := factory(t)
		first := event("first", "A", 1, 1)
		second := event("second", "A", 1, 2)
		appendEvents(t, s, "A", 1, 0, first, second)
		appendEvents(t, s, "A", 1, 0, first, second)
		changed := eventing.NewEventWithID("first", int64(1), "A", "Changed", 1, map[string]any{"value": "different"})
		if err := s.AppendEvents(context.Background(), "A", 1, []eventing.IStorableEvent[int64]{changed, second}, 0); err == nil {
			t.Fatal("same event ID with different content accepted")
		}
		appendEvents(t, s, "A", 1, 2, event("next", "A", 1, 3))
		got, err := s.LoadEvents(context.Background(), "A", 1, 0)
		must(t, err)
		if len(got) != 3 || got[0].Type != first.Type || got[1].ID != second.ID {
			t.Fatalf("retry changed stored history: %+v", got)
		}
	})
	t.Run("invalid_batch_is_atomic", func(t *testing.T) {
		s := factory(t)
		var nilEvent *eventing.Event[int64]
		err := s.AppendEvents(context.Background(), "A", 1, []eventing.IStorableEvent[int64]{event("first", "A", 1, 1), nilEvent}, 0)
		if !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("nil event = %v", err)
		}
		version(t, s, "A", 1, 0)
		appendEvents(t, s, "A", 1, 0, event("first", "A", 1, 1))
	})
	t.Run("cancelled_append", func(t *testing.T) {
		s := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := s.AppendEvents(ctx, "A", 1, []eventing.IStorableEvent[int64]{event("first", "A", 1, 1)}, 0)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled append = %v", err)
		}
		version(t, s, "A", 1, 0)
	})
	t.Run("concurrent_writers", func(t *testing.T) {
		s := factory(t)
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, id := range []string{"writer-1", "writer-2"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				results <- s.AppendEvents(context.Background(), "A", 1, []eventing.IStorableEvent[int64]{event(id, "A", 1, 1)}, 0)
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		successes, conflicts := 0, 0
		for err := range results {
			if err == nil {
				successes++
			} else if errors.Is(err, errors.Concurrency) {
				conflicts++
			} else {
				t.Fatalf("unexpected write error: %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
		}
		version(t, s, "A", 1, 1)
	})
}

func event(id, kind string, aggregateID int64, version uint64) *eventing.Event[int64] {
	e := eventing.NewEventWithID(id, aggregateID, kind, "Changed", version, map[string]any{"value": id})
	e.Timestamp = time.Date(2026, 1, 1, 0, 0, int(version), 0, time.UTC)
	return e
}

func appendEvents(t *testing.T, s store.IEventStore[int64], kind string, id int64, expected uint64, events ...eventing.IStorableEvent[int64]) {
	t.Helper()
	must(t, s.AppendEvents(context.Background(), kind, id, events, expected))
}

func version(t *testing.T, s store.IEventReader[int64], kind string, id int64, want uint64) {
	t.Helper()
	got, err := s.GetAggregateVersion(context.Background(), kind, id)
	must(t, err)
	if got != want {
		t.Fatalf("%s/%d: version=%d, want %d", kind, id, got, want)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
