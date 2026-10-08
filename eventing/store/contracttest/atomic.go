package contracttest

import (
	"context"
	"testing"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
)

// IAtomicBackend 组合原子批量与测试验证所需的单流读写能力。
type IAtomicBackend interface {
	store.IEventStore[int64]
	store.IAtomicEventStore[int64]
}

// RunAtomic 验证跨聚合失败回滚、重试幂等与空事件版本守卫。
func RunAtomic(t *testing.T, factory func(*testing.T) IAtomicBackend) {
	t.Helper()
	t.Run("rollback_retry_and_read_guard", func(t *testing.T) {
		s := factory(t)
		appendEvents(t, s, "A", 2, 0, event("existing", "A", 2, 1))
		batch := []store.EventAppend[int64]{
			{AggregateType: "A", AggregateID: 1, Events: []eventing.IStorableEvent[int64]{event("first", "A", 1, 1)}},
			{AggregateType: "A", AggregateID: 2, Events: []eventing.IStorableEvent[int64]{event("conflict", "A", 2, 1)}},
		}
		if err := s.AppendEventsBatch(context.Background(), batch); !errors.Is(err, errors.Concurrency) {
			t.Fatalf("stale batch = %v", err)
		}
		version(t, s, "A", 1, 0)
		version(t, s, "A", 2, 1)
		batch[1].ExpectedVersion = 1
		batch[1].Events = []eventing.IStorableEvent[int64]{event("next", "A", 2, 2)}
		must(t, s.AppendEventsBatch(context.Background(), batch))
		must(t, s.AppendEventsBatch(context.Background(), batch))
		version(t, s, "A", 1, 1)
		version(t, s, "A", 2, 2)
		guarded := []store.EventAppend[int64]{
			{AggregateType: "A", AggregateID: 3, Events: []eventing.IStorableEvent[int64]{event("third", "A", 3, 1)}},
			{AggregateType: "A", AggregateID: 2, ExpectedVersion: 1},
		}
		if err := s.AppendEventsBatch(context.Background(), guarded); !errors.Is(err, errors.Concurrency) {
			t.Fatalf("stale read guard = %v", err)
		}
		version(t, s, "A", 3, 0)
		guarded[1].ExpectedVersion = 2
		must(t, s.AppendEventsBatch(context.Background(), guarded))
		version(t, s, "A", 3, 1)
		version(t, s, "A", 2, 2)
	})
}
