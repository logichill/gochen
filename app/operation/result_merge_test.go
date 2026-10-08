package operation

import (
	"testing"
)

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

func TestMergeResultDeepClonesMutableFields(t *testing.T) {
	result := &Result{
		Resource:       &Resource{Type: "task", ID: "101"},
		Result:         map[string]any{"nested": map[string]any{"value": "original"}},
		Error:          &OperationError{Details: map[string]any{"items": []any{map[string]any{"value": "original"}}}},
		AffectedScopes: []string{"tasks:list"},
	}

	merged := MergeResult(result, nil, nil)
	result.Resource.ID = "mutated"
	result.Result["nested"].(map[string]any)["value"] = "mutated"
	result.Error.Details["items"].([]any)[0].(map[string]any)["value"] = "mutated"
	result.AffectedScopes[0] = "mutated"

	if merged.Resource.ID != "101" {
		t.Fatalf("expected resource clone, got %+v", merged.Resource)
	}
	if got := merged.Result["nested"].(map[string]any)["value"]; got != "original" {
		t.Fatalf("expected result clone, got %v", got)
	}
	if got := merged.Error.Details["items"].([]any)[0].(map[string]any)["value"]; got != "original" {
		t.Fatalf("expected error details clone, got %v", got)
	}
	if merged.AffectedScopes[0] != "tasks:list" {
		t.Fatalf("expected affected scopes clone, got %+v", merged.AffectedScopes)
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
