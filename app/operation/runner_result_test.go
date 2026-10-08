package operation

import (
	"context"
	"errors"
	"strings"
	"testing"

	gerrors "gochen/errors"
)

func TestRunnerExecuteSanitizesOperationErrorDetails(t *testing.T) {
	runner := NewRunner(nil)
	handlerErr := gerrors.NewCode(gerrors.Internal, "handler failed").
		WithContext("stack", "secret stack").
		WithContext("panic", "secret panic").
		WithContext("panic_stack", "secret panic stack").
		WithContext("error", "raw database error").
		WithContext("field", "status")

	result, err := runner.Execute(context.Background(), &Spec{
		Type: "task.fail",
		Mode: ModeInline,
	}, func(ctx context.Context) (*Result, error) {
		return nil, handlerErr
	})

	if err == nil {
		t.Fatalf("expected handler error")
	}
	if result == nil || result.Error == nil {
		t.Fatalf("expected operation error, got %+v", result)
	}
	if result.Error.Message != "handler failed" {
		t.Fatalf("unexpected public message: %q", result.Error.Message)
	}
	if _, ok := result.Error.Details["stack"]; ok {
		t.Fatalf("stack detail should be filtered: %+v", result.Error.Details)
	}
	if _, ok := result.Error.Details["panic"]; ok {
		t.Fatalf("panic detail should be filtered: %+v", result.Error.Details)
	}
	if _, ok := result.Error.Details["panic_stack"]; ok {
		t.Fatalf("panic_stack detail should be filtered: %+v", result.Error.Details)
	}
	if _, ok := result.Error.Details["error"]; ok {
		t.Fatalf("raw error detail should be filtered: %+v", result.Error.Details)
	}
	if got := result.Error.Details["field"]; got != "status" {
		t.Fatalf("expected safe detail to remain, got %+v", result.Error.Details)
	}
}

func TestRunnerExecuteHidesNonAppErrorMessage(t *testing.T) {
	runner := NewRunner(nil)

	result, err := runner.Execute(context.Background(), &Spec{
		Type: "task.fail",
		Mode: ModeInline,
	}, func(ctx context.Context) (*Result, error) {
		return nil, errors.New("driver secret: password=abc")
	})

	if err == nil {
		t.Fatalf("expected handler error")
	}
	if result == nil || result.Error == nil {
		t.Fatalf("expected operation error, got %+v", result)
	}
	if strings.Contains(result.Error.Message, "password=abc") {
		t.Fatalf("raw non-AppError message leaked: %q", result.Error.Message)
	}
	if result.Error.Message != "operation failed" {
		t.Fatalf("unexpected public message: %q", result.Error.Message)
	}
}

func TestRunnerExecuteMergesResultResourceWithSpecResource(t *testing.T) {
	runner := NewRunner(nil)

	result, err := runner.Execute(context.Background(), &Spec{
		Type:     "task.create",
		Mode:     ModeInline,
		Resource: &Resource{Type: "task"},
	}, func(ctx context.Context) (*Result, error) {
		return &Result{Resource: &Resource{ID: "1001"}}, nil
	})
	if err != nil {
		t.Fatalf("execute inline: %v", err)
	}
	if result.Resource == nil {
		t.Fatalf("expected resource")
	}
	if result.Resource.Type != "task" || result.Resource.ID != "1001" {
		t.Fatalf("unexpected resource: %+v", result.Resource)
	}
}

func TestRunnerExecuteOwnsOperationEnvelope(t *testing.T) {
	runner := NewRunner(&RunnerOptions{
		IDGenerator: func() string { return "op_owned" },
	})

	result, err := runner.Execute(context.Background(), &Spec{
		Type: "task.complete",
		Mode: ModeTracked,
	}, func(ctx context.Context) (*Result, error) {
		return &Result{
			Operation: Operation{
				ID:     "op_handler",
				Type:   "handler.type",
				Mode:   ModeInline,
				Status: StatusFailed,
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("execute tracked: %v", err)
	}
	if result.Operation.ID != "op_owned" {
		t.Fatalf("runner should own operation id, got %q", result.Operation.ID)
	}
	if result.Operation.Type != "task.complete" || result.Operation.Mode != ModeTracked || result.Operation.Status != StatusAccepted {
		t.Fatalf("unexpected runner-owned operation: %+v", result.Operation)
	}
}

func TestRunnerExecutePreservesHandlerSuccessStatus(t *testing.T) {
	runner := NewRunner(&RunnerOptions{
		IDGenerator: func() string { return "op_processing" },
	})

	result, err := runner.Execute(context.Background(), &Spec{
		Type: "task.complete",
		Mode: ModeTracked,
	}, func(ctx context.Context) (*Result, error) {
		return &Result{
			Operation: Operation{Status: StatusProcessing},
		}, nil
	})
	if err != nil {
		t.Fatalf("execute tracked: %v", err)
	}
	if result.Operation.ID != "op_processing" {
		t.Fatalf("runner should still own operation id, got %q", result.Operation.ID)
	}
	if result.Operation.Type != "task.complete" || result.Operation.Mode != ModeTracked {
		t.Fatalf("runner should still own operation identity, got %+v", result.Operation)
	}
	if result.Operation.Status != StatusProcessing {
		t.Fatalf("expected handler status to be preserved, got %s", result.Operation.Status)
	}
}

func TestRunnerExecuteInlineIgnoresHandlerSuccessStatus(t *testing.T) {
	runner := NewRunner(nil)

	result, err := runner.Execute(context.Background(), &Spec{
		Type: "task.create",
		Mode: ModeInline,
	}, func(ctx context.Context) (*Result, error) {
		return &Result{
			Operation: Operation{Status: StatusProcessing},
		}, nil
	})
	if err != nil {
		t.Fatalf("execute inline: %v", err)
	}
	if result.Operation.Status != StatusSettled {
		t.Fatalf("expected inline success to be settled, got %s", result.Operation.Status)
	}
}

func TestRunnerExecuteReturnsFailedEnvelopeOnHandlerError(t *testing.T) {
	runner := NewRunner(&RunnerOptions{
		IDGenerator:         func() string { return "op_failed" },
		StatusURLBuilder:    func(operationID string) string { return "/operations/" + operationID },
		StreamURLBuilder:    func(operationID string) string { return "/operations/" + operationID + "/stream" },
		DefaultRetryAfterMs: 250,
	})

	result, err := runner.Execute(context.Background(), &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		Resource:       &Resource{Type: "task"},
		AffectedScopes: []string{"tasks:list"},
	}, func(ctx context.Context) (*Result, error) {
		return &Result{
			Resource: &Resource{ID: "42"},
			Result:   map[string]any{"attempt": 1},
		}, gerrors.NewCode(gerrors.InvalidInput, "title is required").WithContext("field", "title")
	})
	if err == nil {
		t.Fatalf("expected handler error")
	}
	if result == nil {
		t.Fatalf("expected failed envelope")
	}
	if result.Operation.ID != "op_failed" || result.Operation.Type != "task.complete" || result.Operation.Mode != ModeTracked || result.Operation.Status != StatusFailed {
		t.Fatalf("unexpected failed operation: %+v", result.Operation)
	}
	if result.Error == nil || result.Error.Code != string(gerrors.InvalidInput) || result.Error.Message != "title is required" {
		t.Fatalf("unexpected operation error: %+v", result.Error)
	}
	if result.Resource == nil || result.Resource.Type != "task" || result.Resource.ID != "42" {
		t.Fatalf("unexpected resource merge: %+v", result.Resource)
	}
	if result.StatusURL != "/operations/op_failed" || result.StreamURL != "/operations/op_failed/stream" || result.RetryAfterMs != 250 {
		t.Fatalf("unexpected tracked failure links: %+v", result)
	}
}

func TestRunnerExecuteTracked_StoresFailedEnvelopeOnHandlerError(t *testing.T) {
	store := NewMemoryStore()
	runner := NewRunner(&RunnerOptions{
		Store:            store,
		IDGenerator:      func() string { return "op_failed_stored" },
		StatusURLBuilder: func(operationID string) string { return "/operations/" + operationID },
	})

	result, err := runner.Execute(context.Background(), &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-failed",
	}, func(ctx context.Context) (*Result, error) {
		return &Result{Result: map[string]any{"attempt": 1}}, gerrors.NewCode(gerrors.InvalidInput, "invalid task")
	})
	if err == nil {
		t.Fatalf("expected handler error")
	}
	if result == nil || result.Operation.Status != StatusFailed {
		t.Fatalf("expected failed envelope, got %+v", result)
	}

	stored, getErr := store.Get(context.Background(), "op_failed_stored")
	if getErr != nil {
		t.Fatalf("expected failed operation to be stored: %v", getErr)
	}
	if stored.Operation.Status != StatusFailed || stored.Error == nil || stored.Error.Code != string(gerrors.InvalidInput) {
		t.Fatalf("unexpected stored failure: %+v", stored)
	}

	replayed, replayErr := runner.Execute(context.Background(), &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-failed",
	}, func(ctx context.Context) (*Result, error) {
		t.Fatal("handler should not rerun for stored failed idempotency key")
		return nil, nil
	})
	if replayErr != nil {
		t.Fatalf("replay failed result: %v", replayErr)
	}
	if replayed.Operation.ID != "op_failed_stored" || replayed.Operation.Status != StatusFailed {
		t.Fatalf("expected replayed failed envelope, got %+v", replayed.Operation)
	}
}

func TestRunnerExecuteTrackedReturnsMergedResultWhenStoreFails(t *testing.T) {
	runner := NewRunner(&RunnerOptions{
		Store:       failingStore{},
		IDGenerator: func() string { return "op_store_failed" },
	})
	result, err := runner.Execute(context.Background(), &Spec{
		Type:     "task.create",
		Mode:     ModeTracked,
		Resource: &Resource{Type: "task", ID: "spec"},
	}, func(ctx context.Context) (*Result, error) {
		return &Result{Result: map[string]any{"ok": true}}, nil
	})
	if !gerrors.Is(err, gerrors.Database) {
		t.Fatalf("expected store error, got %v", err)
	}
	if result == nil || result.Operation.ID != "op_store_failed" || result.Resource == nil || result.Resource.ID != "spec" {
		t.Fatalf("expected merged result with operation/resource, got %+v", result)
	}
}

func TestRunnerExecuteClonesHandlerResult(t *testing.T) {
	runner := NewRunner(nil)
	handlerResult := &Result{
		Result:         map[string]any{"nested": map[string]any{"value": "original"}},
		Error:          &OperationError{Details: map[string]any{"items": []any{map[string]any{"value": "original"}}}},
		AffectedScopes: []string{"handler:scope"},
	}

	merged, err := runner.Execute(context.Background(), &Spec{
		Type: "task.create",
		Mode: ModeInline,
	}, func(ctx context.Context) (*Result, error) {
		return handlerResult, nil
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	handlerResult.Result["nested"].(map[string]any)["value"] = "mutated"
	handlerResult.Error.Details["items"].([]any)[0].(map[string]any)["value"] = "mutated"
	handlerResult.AffectedScopes[0] = "mutated"

	if got := merged.Result["nested"].(map[string]any)["value"]; got != "original" {
		t.Fatalf("expected runner result clone, got %v", got)
	}
	if got := merged.Error.Details["items"].([]any)[0].(map[string]any)["value"]; got != "original" {
		t.Fatalf("expected runner error details clone, got %v", got)
	}
	if merged.AffectedScopes[0] != "handler:scope" {
		t.Fatalf("expected runner scopes clone, got %+v", merged.AffectedScopes)
	}
}
