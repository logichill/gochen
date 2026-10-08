package store

import (
	"context"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/internal/testutil"
	"testing"
)

func TestMemoryEventStoreAtomicBatchRollbackAndRetry(t *testing.T) {
	s := NewMemoryEventStore[int64]()
	ctx := context.Background()
	first := testutil.NewEvent[int64](1, "Agg", "Created", 1, nil)
	second := testutil.NewEvent[int64](2, "Agg", "Created", 1, nil)
	if err := s.AppendEvents(ctx, "Agg", 2, []eventing.IStorableEvent[int64]{second}, 0); err != nil {
		t.Fatal(err)
	}
	conflict := testutil.NewEvent[int64](2, "Agg", "Created", 1, nil)
	batch := []EventAppend[int64]{{"Agg", 1, []eventing.IStorableEvent[int64]{first}, 0}, {"Agg", 2, []eventing.IStorableEvent[int64]{conflict}, 0}}
	if err := s.AppendEventsBatch(ctx, batch); !errors.Is(err, errors.Concurrency) {
		t.Fatalf("want conflict: %v", err)
	}
	if version, err := s.GetAggregateVersion(ctx, "Agg", 1); err != nil || version != 0 {
		t.Fatalf("partial append: %d %v", version, err)
	}
	stream, err := s.StreamEvents(ctx, nil)
	if err != nil || len(stream.Events) != 1 {
		t.Fatalf("partial global stream: %+v %v", stream, err)
	}
	secondUpdate := testutil.NewEvent[int64](2, "Agg", "Updated", 2, nil)
	batch[1] = EventAppend[int64]{"Agg", 2, []eventing.IStorableEvent[int64]{secondUpdate}, 1}
	for range 2 {
		if err := s.AppendEventsBatch(ctx, batch); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[int64]uint64{1: 1, 2: 2} {
		if version, err := s.GetAggregateVersion(ctx, "Agg", id); err != nil || version != want {
			t.Fatalf("version: %d %v", version, err)
		}
	}
	third := testutil.NewEvent[int64](3, "Agg", "Created", 1, nil)
	if err := s.AppendEventsBatch(ctx, []EventAppend[int64]{{AggregateType: "Agg", AggregateID: 3, Events: []eventing.IStorableEvent[int64]{third}}, {AggregateType: "Agg", AggregateID: 2, ExpectedVersion: 1}}); !errors.Is(err, errors.Concurrency) {
		t.Fatalf("stale read guard: %v", err)
	}
	if version, err := s.GetAggregateVersion(ctx, "Agg", 3); err != nil || version != 0 {
		t.Fatalf("guard failure persisted earlier write: %d %v", version, err)
	}

}
