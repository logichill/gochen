package operation

import (
	"context"
	"errors"
	"strings"
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

func TestDefaultOperationIDNonEmpty(t *testing.T) {
	id := defaultOperationID()
	if !strings.HasPrefix(id, "op_") || strings.TrimSpace(id) == "op_" {
		t.Fatalf("expected non-empty operation id with op_ prefix, got %q", id)
	}
	if len(strings.TrimPrefix(id, "op_")) != 36 {
		t.Fatalf("expected UUID operation id, got %q", id)
	}
}

func TestFallbackOperationIDNonEmptyAndUnique(t *testing.T) {
	first := fallbackOperationID()
	second := fallbackOperationID()
	if !strings.HasPrefix(first, "op_") || !strings.HasPrefix(second, "op_") {
		t.Fatalf("expected fallback operation ids with op_ prefix, got %q and %q", first, second)
	}
	if first == second {
		t.Fatalf("expected unique fallback operation ids, got %q", first)
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

	for _, typ := range []string{"", "   "} {
		t.Run("type="+typ, func(t *testing.T) {
			if _, err := runner.Execute(context.Background(), &Spec{Type: typ, Mode: ModeInline}, func(ctx context.Context) (*Result, error) {
				return &Result{}, nil
			}); err == nil {
				t.Fatalf("expected invalid spec error")
			}
		})
	}
}

func TestRunnerExecuteCancelledContextDoesNotRunHandler(t *testing.T) {
	runner := NewRunner(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	_, err := runner.Execute(ctx, &Spec{Type: "task.create", Mode: ModeInline}, func(ctx context.Context) (*Result, error) {
		called = true
		return &Result{}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if called {
		t.Fatalf("handler should not run when context is already canceled")
	}
}
