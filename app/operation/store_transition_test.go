package operation

import (
	"context"
	"reflect"
	"testing"

	"gochen/errors"
)

func TestTrackerRefreshPreservesConcurrentResult(t *testing.T) {
	for _, status := range []Status{StatusAccepted, StatusSettled, StatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			ctx := context.Background()
			store := NewMemoryStore()
			original := &Result{Operation: Operation{ID: "op", Status: StatusAccepted}, Result: map[string]any{"value": "old"}}
			if err := store.Put(ctx, original); err != nil {
				t.Fatal(err)
			}
			checking, release := make(chan struct{}), make(chan struct{})
			tracker := NewTracker(&TrackerOptions{Store: store, Settlement: SettlementCheckerFunc(func(_ context.Context, input *Result) (bool, error) {
				close(checking)
				<-release
				input.Result["value"] = "checker mutation"
				return false, nil
			})})
			type outcome struct {
				result *Result
				err    error
			}
			done := make(chan outcome, 1)
			go func() { r, err := tracker.Refresh(ctx, original); done <- outcome{r, err} }()
			waitForSignal(t, checking, "checker did not start")
			latest := CloneResult(original)
			latest.Operation.Status = status
			latest.Result["value"] = "new"
			err := store.Put(ctx, latest)
			close(release)
			if err != nil {
				t.Fatal(err)
			}
			got := <-done
			if got.err != nil {
				t.Fatal(got.err)
			}
			wantStatus := status
			if status == StatusAccepted {
				wantStatus = StatusProcessing
			}
			if got.result.Operation.Status != wantStatus || got.result.Result["value"] != "new" {
				t.Fatalf("stale refresh replaced current result: %+v", got.result)
			}
			stored, err := store.Get(ctx, "op")
			if err != nil || !reflect.DeepEqual(stored, got.result) {
				t.Fatalf("stored=%+v, err=%v", stored, err)
			}
			if original.Result["value"] != "old" {
				t.Fatal("checker mutated caller result")
			}
		})
	}
}

func TestTrackerRejectsUnsafeStoreBeforeChecking(t *testing.T) {
	tracker := NewTracker(&TrackerOptions{Store: &baseOnlyStore{inner: NewMemoryStore()}, Settlement: SettlementCheckerFunc(func(context.Context, *Result) (bool, error) {
		t.Fatal("unsupported store must be detected before settlement checking")
		return false, nil
	})})
	_, err := tracker.Refresh(context.Background(), &Result{Operation: Operation{ID: "op", Status: StatusAccepted}})
	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("got %v", err)
	}
}

func TestRecoveryPreservesExistingResultAndReservationOwnership(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	original := &Result{Operation: Operation{ID: "op", Status: StatusAccepted}, Result: map[string]any{"value": "old"}}
	if err := store.ReserveIdempotencyKey(ctx, "key", "op"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "wrong"} {
		if _, err := store.RecoverResult(ctx, key, original); !errors.Is(err, errors.Conflict) {
			t.Fatalf("key %q: %v", key, err)
		}
	}
	if _, err := store.TransitionStatus(ctx, "op", StatusAccepted, StatusSettled); !IsStoreNotFound(err) {
		t.Fatalf("reservation became visible: %v", err)
	}
	recovered, err := store.RecoverResult(ctx, "key", original)
	if err != nil {
		t.Fatal(err)
	}
	recovered.Operation.Status = StatusSettled
	recovered.Result["value"] = "new"
	if err := store.Put(ctx, recovered); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"key", ""} {
		got, err := store.RecoverResult(ctx, key, original)
		if err != nil || !reflect.DeepEqual(got, recovered) {
			t.Fatalf("late recovery: %+v, %v", got, err)
		}
		got.Result["value"] = "caller mutation"
	}
	byKey, err := store.GetByIdempotencyKey(ctx, "key")
	if err != nil || !reflect.DeepEqual(byKey, recovered) {
		t.Fatalf("key binding: %+v, %v", byKey, err)
	}
	if err := store.ReserveIdempotencyKey(ctx, "new-key", "op"); !errors.Is(err, errors.Conflict) {
		t.Fatalf("reused operation ID: %v", err)
	}
	if got, err := store.Get(ctx, "op"); err != nil || !reflect.DeepEqual(got, recovered) {
		t.Fatalf("reservation erased result: %+v %v", got, err)
	}
	other := CloneResult(original)
	other.Operation.ID = "other"
	if _, err := store.RecoverResult(ctx, "key", other); !errors.Is(err, errors.Conflict) {
		t.Fatalf("stole key: %v", err)
	}
	if err := store.Delete(ctx, "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecoverResult(ctx, "key", original); !errors.Is(err, errors.Conflict) {
		t.Fatalf("revived deleted operation: %v", err)
	}
	if _, err := store.TransitionStatus(ctx, "op", StatusAccepted, StatusSettled); !IsStoreNotFound(err) {
		t.Fatalf("revived deleted operation: %v", err)
	}
}

func TestMemoryStoreStatusTransitionRejectsRollback(t *testing.T) {
	store := NewMemoryStore()
	for _, pair := range [][2]Status{{StatusProcessing, StatusAccepted}, {StatusSettled, StatusFailed}, {StatusAccepted, "invalid"}} {
		if _, err := store.TransitionStatus(context.Background(), "op", pair[0], pair[1]); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("%v: %v", pair, err)
		}
	}
}

func TestOperationEntryPointsRejectNilDependenciesAndContext(t *testing.T) {
	var store *MemoryStore
	runner := NewRunner(&RunnerOptions{Store: store})
	_, err := runner.Execute(context.Background(), &Spec{Type: "test", Mode: ModeTracked}, func(context.Context) (*Result, error) {
		t.Fatal("handler ran with invalid store")
		return nil, nil
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatal(err)
	}
	tracker := NewTracker(&TrackerOptions{Store: store})
	input := &Result{Operation: Operation{ID: "op", Status: StatusAccepted}}
	if _, err := tracker.Load(context.Background(), "op"); err == nil {
		t.Fatal("nil store accepted")
	}
	if _, err := tracker.Refresh(context.Background(), input); !errors.Is(err, errors.InvalidInput) {
		t.Fatal(err)
	}
	if _, err := NewTracker(nil).Refresh(nil, input); !errors.Is(err, errors.InvalidInput) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewTracker(nil).Refresh(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
