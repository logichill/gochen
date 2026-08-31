package operation

import (
	"context"
	"testing"
)

func TestTrackerLoadRefreshesTrackedOperation(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Put(context.Background(), &Result{
		Operation: Operation{
			ID:     "op_1",
			Type:   "task.create",
			Mode:   ModeTracked,
			Status: StatusAccepted,
		},
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	tracker := NewTracker(&TrackerOptions{
		Store: store,
		Settlement: SettlementCheckerFunc(func(ctx context.Context, result *Result) (bool, error) {
			return false, nil
		}),
	})

	got, err := tracker.Load(context.Background(), "op_1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Operation.Status != StatusProcessing {
		t.Fatalf("unexpected status: %s", got.Operation.Status)
	}
}

func TestTrackerRefreshSettlesTrackedOperation(t *testing.T) {
	store := NewMemoryStore()
	tracker := NewTracker(&TrackerOptions{
		Store: store,
		Settlement: SettlementCheckerFunc(func(ctx context.Context, result *Result) (bool, error) {
			return true, nil
		}),
	})

	got, err := tracker.Refresh(context.Background(), &Result{
		Operation: Operation{
			ID:     "op_2",
			Type:   "task.complete",
			Mode:   ModeTracked,
			Status: StatusAccepted,
		},
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got.Operation.Status != StatusSettled {
		t.Fatalf("unexpected status: %s", got.Operation.Status)
	}

	stored, err := store.Get(context.Background(), "op_2")
	if err != nil {
		t.Fatalf("stored get: %v", err)
	}
	if stored.Operation.Status != StatusSettled {
		t.Fatalf("unexpected stored status: %s", stored.Operation.Status)
	}
}

func TestTrackerRefreshReturnsTerminalResultAsIs(t *testing.T) {
	tracker := NewTracker(nil)
	input := &Result{
		Operation: Operation{
			ID:     "op_3",
			Type:   "task.evaluate",
			Mode:   ModeTracked,
			Status: StatusSettled,
		},
	}

	got, err := tracker.Refresh(context.Background(), input)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got != input {
		t.Fatalf("expected terminal result passthrough")
	}
}
