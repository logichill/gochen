package operation

import (
	"context"
	"fmt"
	"testing"

	"gochen/contextx"
	"gochen/errors"
)

func TestRunnerPreservesCommittedFailure(t *testing.T) {
	for _, mode := range []Mode{ModeInline, ModeTracked} {
		t.Run(string(mode), func(t *testing.T) {
			ctx := context.Background()
			store := NewMemoryStore()
			runner := NewRunner(&RunnerOptions{Store: store})
			cause := errors.NewCode(errors.Dependency, "notification failed")
			result, err := runner.Execute(ctx, &Spec{Type: "task.complete", Mode: mode}, func(context.Context) (*Result, error) {
				return &Result{Resource: &Resource{Type: "task", ID: "42"}}, fmt.Errorf("service: %w", contextx.WrapAfterCommitError(cause))
			})
			if !contextx.IsAfterCommitError(err) || !errors.Is(err, cause) {
				t.Fatalf("lost committed error: %v", err)
			}
			if result.Operation.Status != StatusDegraded || result.Resource.ID != "42" || result.Error == nil {
				t.Fatalf("lost committed result: %+v", result)
			}
			if mode == ModeTracked {
				loaded, err := store.Get(ctx, result.Operation.ID)
				if err != nil || loaded.Operation.Status != StatusDegraded {
					t.Fatalf("stored result: %+v %v", loaded, err)
				}
				tracker := NewTracker(&TrackerOptions{Store: store, Settlement: SettlementCheckerFunc(func(context.Context, *Result) (bool, error) {
					t.Fatal("degraded must remain terminal")
					return true, nil
				})})
				if _, err := tracker.Load(ctx, result.Operation.ID); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
