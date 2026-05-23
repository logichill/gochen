package operation

import (
	"context"
	"testing"
)

func TestRunnerExecuteInline(t *testing.T) {
	runner := NewRunner(nil)

	result, err := runner.Execute(context.Background(), &Spec{
		Type:           "task.create",
		Mode:           ModeInline,
		AffectedScopes: []string{"tasks:list"},
		Resource:       &Resource{Type: "task"},
	}, func(ctx context.Context) (*Result, error) {
		if got := OperationIDFromContext(ctx); got != "" {
			t.Fatalf("inline operation should not inject operation id, got %q", got)
		}
		return &Result{
			Resource: &Resource{Type: "task", ID: "1001"},
			Result:   map[string]any{"task_id": int64(1001)},
		}, nil
	})
	if err != nil {
		t.Fatalf("execute inline: %v", err)
	}
	if result.Operation.Type != "task.create" {
		t.Fatalf("unexpected operation type: %s", result.Operation.Type)
	}
	if result.Operation.Mode != ModeInline {
		t.Fatalf("unexpected mode: %s", result.Operation.Mode)
	}
	if result.Operation.Status != StatusSettled {
		t.Fatalf("unexpected status: %s", result.Operation.Status)
	}
	if result.Operation.ID != "" {
		t.Fatalf("inline operation should not have generated id, got %q", result.Operation.ID)
	}
	if result.Resource == nil || result.Resource.ID != "1001" {
		t.Fatalf("unexpected resource: %+v", result.Resource)
	}
	if len(result.AffectedScopes) != 1 || result.AffectedScopes[0] != "tasks:list" {
		t.Fatalf("unexpected scopes: %+v", result.AffectedScopes)
	}
}

func TestRunnerExecuteTracked(t *testing.T) {
	runner := NewRunner(&RunnerOptions{
		IDGenerator:      func() string { return "op_fixed" },
		StatusURLBuilder: func(operationID string) string { return "/operations/" + operationID },
		StreamURLBuilder: func(operationID string) string { return "/operations/" + operationID + "/stream" },
	})

	result, err := runner.Execute(context.Background(), &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		AffectedScopes: []string{"tasks:list", "points:1"},
	}, func(ctx context.Context) (*Result, error) {
		if got := OperationIDFromContext(ctx); got != "op_fixed" {
			t.Fatalf("unexpected operation id in context: %q", got)
		}
		return &Result{
			Resource: &Resource{Type: "task", ID: "42"},
		}, nil
	})
	if err != nil {
		t.Fatalf("execute tracked: %v", err)
	}
	if result.Operation.ID != "op_fixed" {
		t.Fatalf("unexpected operation id: %s", result.Operation.ID)
	}
	if result.Operation.Status != StatusAccepted {
		t.Fatalf("unexpected status: %s", result.Operation.Status)
	}
	if result.StatusURL != "/operations/op_fixed" {
		t.Fatalf("unexpected status url: %s", result.StatusURL)
	}
	if result.StreamURL != "/operations/op_fixed/stream" {
		t.Fatalf("unexpected stream url: %s", result.StreamURL)
	}
}

func TestRunnerExecuteRejectsInvalidSpec(t *testing.T) {
	runner := NewRunner(nil)

	if _, err := runner.Execute(context.Background(), &Spec{Type: "", Mode: ModeInline}, func(ctx context.Context) (*Result, error) {
		return &Result{}, nil
	}); err == nil {
		t.Fatalf("expected invalid spec error")
	}
}

func TestMergeResultPrefersExplicitResourceAndFallsBackToSpec(t *testing.T) {
	spec := &Spec{
		Type:     "task.create",
		Mode:     ModeInline,
		Resource: &Resource{Type: "task"},
	}

	merged := MergeResult(&Result{
		Result: map[string]any{"ok": true},
	}, spec, &Resource{ID: "101"})

	if merged.Resource == nil {
		t.Fatalf("expected resource to be merged")
	}
	if merged.Resource.Type != "task" {
		t.Fatalf("unexpected merged resource type: %s", merged.Resource.Type)
	}
	if merged.Resource.ID != "101" {
		t.Fatalf("unexpected merged resource id: %s", merged.Resource.ID)
	}
}

func TestMergeResultPreservesExistingResourceFields(t *testing.T) {
	spec := &Spec{
		Type:     "task.create",
		Mode:     ModeInline,
		Resource: &Resource{Type: "spec-task", ID: "spec-id"},
	}

	merged := MergeResult(&Result{
		Resource: &Resource{Type: "result-task"},
	}, spec, &Resource{Type: "resource-task", ID: "resource-id"})

	if merged.Resource == nil {
		t.Fatalf("expected resource to be merged")
	}
	if merged.Resource.Type != "result-task" {
		t.Fatalf("unexpected merged resource type: %s", merged.Resource.Type)
	}
	if merged.Resource.ID != "resource-id" {
		t.Fatalf("unexpected merged resource id: %s", merged.Resource.ID)
	}
}
