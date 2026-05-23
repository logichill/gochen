package subscription

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/eventing"
	"gochen/eventing/store"
)

// TestSubscription_Run_ConsumesEvents 验证 Subscription Run ConsumesEvents。
func TestSubscription_Run_ConsumesEvents(t *testing.T) {
	es := store.NewMemoryEventStore()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu     sync.Mutex
		types  []string
		seenID = make(map[string]struct{})
	)
	sub, err := New[int64](es, func(ctx context.Context, evt eventing.Event[int64]) error {
		mu.Lock()
		defer mu.Unlock()
		if _, ok := seenID[evt.GetID()]; ok {
			t.Fatalf("duplicate event delivered: %s", evt.GetID())
		}
		seenID[evt.GetID()] = struct{}{}
		types = append(types, evt.GetType())
		if len(types) == 2 {
			cancel()
		}
		return nil
	}, &Config{
		PollInterval: 5 * time.Millisecond,
		BatchSize:    10,
		FromTime:     time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	// 先写入两条事件
	e1 := eventing.NewEvent[int64](1, "Agg", "A", 1, map[string]any{"x": 1})
	e2 := eventing.NewEvent[int64](1, "Agg", "B", 2, map[string]any{"x": 2})
	if err := es.AppendEvents(context.Background(), 1, []eventing.IStorableEvent[int64]{e1, e2}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	_ = sub.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(types) != 2 {
		t.Fatalf("expected 2 events, got %d", len(types))
	}
	if types[0] != "A" || types[1] != "B" {
		t.Fatalf("unexpected event types: %v", types)
	}
	if sub.Cursor() == "" {
		t.Fatalf("expected cursor to be set")
	}
}
