package operation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gerrors "gochen/errors"
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

func TestRunnerExecuteTracked_ReusesIdempotencyKeyResult(t *testing.T) {
	store := NewMemoryStore()
	ids := []string{"op_first", "op_second"}
	calls := 0
	runner := NewRunner(&RunnerOptions{
		Store: store,
		IDGenerator: func() string {
			id := ids[calls]
			return id
		},
	})
	spec := &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-1",
	}

	first, err := runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
		calls++
		return &Result{Result: map[string]any{"call": calls}}, nil
	})
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	second, err := runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
		calls++
		return &Result{Result: map[string]any{"call": calls}}, nil
	})
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected handler to run once, got %d", calls)
	}
	if first.Operation.ID != "op_first" || second.Operation.ID != "op_first" {
		t.Fatalf("expected idempotent result to reuse op_first, got %q and %q", first.Operation.ID, second.Operation.ID)
	}
}

func TestRunnerExecuteTracked_ReservesIdempotencyKeyBeforeHandler(t *testing.T) {
	store := NewMemoryStore()
	ids := []string{"op_first", "op_second"}
	calls := 0
	runner := NewRunner(&RunnerOptions{
		Store: store,
		IDGenerator: func() string {
			id := ids[calls]
			return id
		},
	})
	spec := &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-reserve",
	}

	result, err := runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
		calls++
		if got := OperationIDFromContext(ctx); got != "op_first" {
			t.Fatalf("expected reserved operation id in context, got %q", got)
		}
		store.mu.RLock()
		reservedID := store.idempotencyKeys[spec.IdempotencyKey]
		reserved := store.records[reservedID]
		_, reservationMarked := store.idempotencyReservations[reservedID]
		store.mu.RUnlock()
		if reservedID != "op_first" {
			t.Fatalf("expected idempotency key to reserve op_first, got %q", reservedID)
		}
		if !reservationMarked || reserved == nil {
			t.Fatalf("expected reserved operation placeholder, got %+v", reserved)
		}
		return &Result{Result: map[string]any{"call": calls}}, nil
	})
	if err != nil {
		t.Fatalf("execute reserved: %v", err)
	}
	if result.Operation.ID != "op_first" {
		t.Fatalf("expected op_first, got %q", result.Operation.ID)
	}

	replayed, err := runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
		t.Fatal("handler should not rerun after reservation has a stored result")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("execute replay: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected handler to run once, got %d", calls)
	}
	if replayed.Operation.ID != "op_first" {
		t.Fatalf("expected replayed op_first, got %q", replayed.Operation.ID)
	}
}

func TestRunnerExecuteTracked_IdempotencyKeySingleFlightsConcurrentCalls(t *testing.T) {
	store := NewMemoryStore()
	var idSeq atomic.Uint64
	runner := NewRunner(&RunnerOptions{
		Store: store,
		IDGenerator: func() string {
			return "op_concurrent"
		},
	})
	spec := &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-concurrent",
	}

	start := make(chan struct{})
	ownerStarted := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	var wg sync.WaitGroup
	results := make(chan *Result, 20)
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
				if calls.Add(1) == 1 {
					close(ownerStarted)
				}
				<-release
				return &Result{Result: map[string]any{"id": idSeq.Add(1)}}, nil
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}

	close(start)
	waitForSignal(t, ownerStarted, "timed out waiting for first handler call")
	close(release)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("execute: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected handler to run once, got %d", calls.Load())
	}
	for result := range results {
		if result.Operation.ID != "op_concurrent" {
			t.Fatalf("expected shared operation id, got %q", result.Operation.ID)
		}
		if result.Result["id"] != uint64(1) {
			t.Fatalf("expected shared result payload, got %+v", result.Result)
		}
	}
}

func TestRunnerExecuteTracked_IdempotencyKeySingleFlightsStoreWithoutExecutor(t *testing.T) {
	store := idempotencyOnlyStore{inner: NewMemoryStore()}
	var idSeq atomic.Uint64
	runner := NewRunner(&RunnerOptions{
		Store: store,
		IDGenerator: func() string {
			return "op_fallback"
		},
	})
	spec := &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-fallback",
	}

	start := make(chan struct{})
	ownerStarted := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	var wg sync.WaitGroup
	results := make(chan *Result, 10)
	errs := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
				if calls.Add(1) == 1 {
					close(ownerStarted)
				}
				<-release
				return &Result{Result: map[string]any{"id": idSeq.Add(1)}}, nil
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}

	close(start)
	waitForSignal(t, ownerStarted, "timed out waiting for first handler call")
	close(release)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("execute: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected fallback handler to run once, got %d", calls.Load())
	}
	for result := range results {
		if result.Operation.ID != "op_fallback" {
			t.Fatalf("expected shared operation id, got %q", result.Operation.ID)
		}
		if result.Result["id"] != uint64(1) {
			t.Fatalf("expected shared result payload, got %+v", result.Result)
		}
	}
}

func TestRunnerExecuteTracked_IdempotencyKeyRequiresIdempotencyStore(t *testing.T) {
	tests := []struct {
		name  string
		store IStore
	}{
		{name: "no store"},
		{name: "base store only", store: &baseOnlyStore{inner: NewMemoryStore()}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := NewRunner(&RunnerOptions{
				Store:       tc.store,
				IDGenerator: func() string { return "op_should_not_allocate" },
			})
			called := false
			_, err := runner.Execute(context.Background(), &Spec{
				Type:           "task.complete",
				Mode:           ModeTracked,
				IdempotencyKey: "request-missing-store",
			}, func(ctx context.Context) (*Result, error) {
				called = true
				return &Result{}, nil
			})
			if !gerrors.Is(err, gerrors.InvalidInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
			if called {
				t.Fatalf("handler should not run without idempotency store")
			}
		})
	}
}

func TestRunnerExecuteTracked_IdempotencyKeyReleasesAfterPanic(t *testing.T) {
	store := NewMemoryStore()
	ids := []string{"op_before_panic", "op_after_panic"}
	var idSeq atomic.Int32
	runner := NewRunner(&RunnerOptions{
		Store:       store,
		IDGenerator: func() string { return ids[idSeq.Add(1)-1] },
	})
	spec := &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-panic",
	}

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("expected panic from handler")
			}
		}()
		_, _ = runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
			panic("boom")
		})
	}()

	result, err := runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
		return &Result{Result: map[string]any{"ok": true}}, nil
	})
	if err != nil {
		t.Fatalf("retry after panic: %v", err)
	}
	if result.Operation.ID != "op_after_panic" {
		t.Fatalf("unexpected operation id after retry: %q", result.Operation.ID)
	}
	if result.Result["ok"] != true {
		t.Fatalf("unexpected retry result: %+v", result.Result)
	}
}

func TestRunnerExecuteTracked_IdempotencyKeyPanicCleanupIgnoresCanceledRequestContext(t *testing.T) {
	store := NewMemoryStore()
	ids := []string{"op_before_panic", "op_after_panic"}
	var idSeq atomic.Int32
	runner := NewRunner(&RunnerOptions{
		Store:       store,
		IDGenerator: func() string { return ids[idSeq.Add(1)-1] },
	})
	spec := &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-panic-canceled",
	}

	ctx, cancel := context.WithCancel(context.Background())
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("expected panic from handler")
			}
		}()
		_, _ = runner.Execute(ctx, spec, func(context.Context) (*Result, error) {
			cancel()
			panic("boom")
		})
	}()

	result, err := runner.Execute(context.Background(), spec, func(context.Context) (*Result, error) {
		return &Result{Result: map[string]any{"ok": true}}, nil
	})
	if err != nil {
		t.Fatalf("retry after panic with canceled ctx: %v", err)
	}
	if result.Operation.ID != "op_after_panic" {
		t.Fatalf("unexpected operation id after retry: %q", result.Operation.ID)
	}
}

func TestRunnerExecuteTracked_ConflictFallsBackToStoredIdempotencyResult(t *testing.T) {
	store := newConflictOnPutStore()
	existing := &Result{
		Operation: Operation{
			ID:     "op_existing",
			Type:   "task.complete",
			Mode:   ModeTracked,
			Status: StatusAccepted,
		},
		Result: map[string]any{"ok": true},
	}
	if err := store.MemoryStore.PutWithIdempotencyKey(context.Background(), "request-conflict", existing); err != nil {
		t.Fatalf("seed existing result: %v", err)
	}

	runner := NewRunner(&RunnerOptions{
		Store:       store,
		IDGenerator: func() string { return "op_new" },
	})
	spec := &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-conflict",
	}

	result, err := runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
		return &Result{Result: map[string]any{"new": true}}, nil
	})
	if err != nil {
		t.Fatalf("execute with put conflict: %v", err)
	}
	if result.Operation.ID != "op_existing" {
		t.Fatalf("expected existing operation to win after conflict, got %q", result.Operation.ID)
	}
}

func TestRunnerExecuteTracked_HandlerErrorConflictReplaysStoredIdempotencyResult(t *testing.T) {
	store := &lateConflictOnPutStore{
		existing: &Result{
			Operation: Operation{
				ID:     "op_existing",
				Type:   "task.complete",
				Mode:   ModeTracked,
				Status: StatusAccepted,
			},
			Result: map[string]any{"winner": "existing"},
		},
	}
	runner := NewRunner(&RunnerOptions{
		Store:       store,
		IDGenerator: func() string { return "op_new" },
	})
	spec := &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-late-conflict",
	}

	result, err := runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
		return &Result{Result: map[string]any{"winner": "local"}}, gerrors.NewCode(gerrors.InvalidInput, "local handler failed")
	})
	if err != nil {
		t.Fatalf("expected conflict replay to suppress local handler error, got %v", err)
	}
	if result.Operation.ID != "op_existing" {
		t.Fatalf("expected existing operation to win after conflict, got %q", result.Operation.ID)
	}
	if result.Result["winner"] != "existing" {
		t.Fatalf("expected existing result payload, got %+v", result.Result)
	}
	if store.putCalls != 1 {
		t.Fatalf("expected one conflicted put, got %d", store.putCalls)
	}
}

func TestRunnerExecuteTracked_ReleasesReservationWhenStoreResultFails(t *testing.T) {
	store := newFailOnceOnPutStore()
	ids := []string{"op_failed_put", "op_retry"}
	var idSeq atomic.Int32
	runner := NewRunner(&RunnerOptions{
		Store: store,
		IDGenerator: func() string {
			return ids[idSeq.Add(1)-1]
		},
	})
	spec := &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-store-failure",
	}

	_, err := runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
		return &Result{Result: map[string]any{"attempt": 1}}, nil
	})
	if !gerrors.Is(err, gerrors.Database) {
		t.Fatalf("expected first store failure, got %v", err)
	}
	if _, err := store.GetByIdempotencyKey(context.Background(), spec.IdempotencyKey); !gerrors.Is(err, gerrors.NotFound) {
		t.Fatalf("expected failed reservation to be released, got %v", err)
	}

	result, err := runner.Execute(context.Background(), spec, func(ctx context.Context) (*Result, error) {
		return &Result{Result: map[string]any{"attempt": 2}}, nil
	})
	if err != nil {
		t.Fatalf("retry after store failure: %v", err)
	}
	if result.Operation.ID != "op_retry" {
		t.Fatalf("expected retry operation id, got %q", result.Operation.ID)
	}
	if result.Result["attempt"] != 2 {
		t.Fatalf("expected retry result, got %+v", result.Result)
	}
}

func TestRunnerExecuteTracked_ReservationConflictSkipsHandler(t *testing.T) {
	store := NewMemoryStore()
	existing := &Result{
		Operation: Operation{ID: "op_existing", Type: "task.complete", Mode: ModeTracked, Status: StatusAccepted},
		Result:    map[string]any{"ok": true},
	}
	if err := store.PutWithIdempotencyKey(context.Background(), "request-reservation-conflict", existing); err != nil {
		t.Fatalf("seed existing result: %v", err)
	}

	runner := NewRunner(&RunnerOptions{
		Store:       store,
		IDGenerator: func() string { return "op_new" },
	})
	result, err := runner.Execute(context.Background(), &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-reservation-conflict",
	}, func(ctx context.Context) (*Result, error) {
		t.Fatal("handler should not run when reservation finds existing idempotency result")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("execute with reservation conflict: %v", err)
	}
	if result.Operation.ID != "op_existing" {
		t.Fatalf("expected existing operation to win, got %q", result.Operation.ID)
	}
}

func TestRunnerExecuteTracked_ReservationConflictWithoutResultDoesNotRunHandler(t *testing.T) {
	store := NewMemoryStore()
	if err := store.ReserveIdempotencyKey(context.Background(), "request-inflight", "op_inflight"); err != nil {
		t.Fatalf("reserve inflight: %v", err)
	}

	runner := NewRunner(&RunnerOptions{
		Store:       store,
		IDGenerator: func() string { return "op_new" },
	})
	_, err := runner.Execute(context.Background(), &Spec{
		Type:           "task.complete",
		Mode:           ModeTracked,
		IdempotencyKey: "request-inflight",
	}, func(ctx context.Context) (*Result, error) {
		t.Fatal("handler should not run while another operation owns the idempotency key")
		return nil, nil
	})
	if !gerrors.Is(err, gerrors.Conflict) {
		t.Fatalf("expected conflict for in-flight reservation, got %v", err)
	}
}

type idempotencyOnlyStore struct {
	inner *MemoryStore
}

type baseOnlyStore struct {
	inner *MemoryStore
}

type conflictOnPutStore struct {
	*MemoryStore
	conflicted map[string]bool
}

type lateConflictOnPutStore struct {
	existing *Result
	getCalls int
	putCalls int
}

type failOnceOnPutStore struct {
	*MemoryStore
	failed bool
}

type failingStore struct{}

type staleReservationStore struct {
	*MemoryStore
	reserveCalls int
}

func newConflictOnPutStore() *conflictOnPutStore {
	return &conflictOnPutStore{
		MemoryStore: NewMemoryStore(),
		conflicted:  make(map[string]bool),
	}
}

func newFailOnceOnPutStore() *failOnceOnPutStore {
	return &failOnceOnPutStore{MemoryStore: NewMemoryStore()}
}

func (s *conflictOnPutStore) PutWithIdempotencyKey(ctx context.Context, key string, result *Result) error {
	if key != "" && !s.conflicted[key] {
		s.conflicted[key] = true
		return gerrors.NewCode(gerrors.Conflict, "simulated put conflict")
	}
	return s.MemoryStore.PutWithIdempotencyKey(ctx, key, result)
}

func (s *lateConflictOnPutStore) Get(ctx context.Context, id string) (*Result, error) {
	return nil, gerrors.NewCode(gerrors.NotFound, "operation not found")
}

func (s *lateConflictOnPutStore) Put(ctx context.Context, result *Result) error {
	return gerrors.NewCode(gerrors.Database, "unexpected non-idempotent put")
}

func (s *lateConflictOnPutStore) Delete(ctx context.Context, id string) error {
	return nil
}

func (s *lateConflictOnPutStore) GetByIdempotencyKey(ctx context.Context, key string) (*Result, error) {
	s.getCalls++
	if s.getCalls == 1 {
		return nil, gerrors.NewCode(gerrors.NotFound, "operation not found")
	}
	return CloneResult(s.existing), nil
}

func (s *lateConflictOnPutStore) PutWithIdempotencyKey(ctx context.Context, key string, result *Result) error {
	s.putCalls++
	return gerrors.NewCode(gerrors.Conflict, "simulated late put conflict")
}

func (s *failOnceOnPutStore) PutWithIdempotencyKey(ctx context.Context, key string, result *Result) error {
	if key != "" && !s.failed {
		s.failed = true
		return gerrors.NewCode(gerrors.Database, "simulated store failure")
	}
	return s.MemoryStore.PutWithIdempotencyKey(ctx, key, result)
}

func (s failingStore) Get(ctx context.Context, id string) (*Result, error) {
	return nil, gerrors.NewCode(gerrors.NotFound, "operation not found")
}

func (s failingStore) Put(ctx context.Context, result *Result) error {
	return gerrors.NewCode(gerrors.Database, "store failed")
}

func (s failingStore) Delete(ctx context.Context, id string) error {
	return nil
}

func (s *staleReservationStore) ReserveIdempotencyKey(ctx context.Context, key string, operationID string) error {
	s.reserveCalls++
	if s.reserveCalls == 1 {
		return gerrors.NewCode(gerrors.Conflict, "stale reservation")
	}
	return s.MemoryStore.ReserveIdempotencyKey(ctx, key, operationID)
}

func (s idempotencyOnlyStore) Get(ctx context.Context, id string) (*Result, error) {
	return s.inner.Get(ctx, id)
}

func (s idempotencyOnlyStore) Put(ctx context.Context, result *Result) error {
	return s.inner.Put(ctx, result)
}

func (s idempotencyOnlyStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

func (s idempotencyOnlyStore) GetByIdempotencyKey(ctx context.Context, key string) (*Result, error) {
	return s.inner.GetByIdempotencyKey(ctx, key)
}

func (s idempotencyOnlyStore) PutWithIdempotencyKey(ctx context.Context, key string, result *Result) error {
	return s.inner.PutWithIdempotencyKey(ctx, key, result)
}

func (s *baseOnlyStore) Get(ctx context.Context, id string) (*Result, error) {
	return s.inner.Get(ctx, id)
}

func (s *baseOnlyStore) Put(ctx context.Context, result *Result) error {
	return s.inner.Put(ctx, result)
}

func (s *baseOnlyStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
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

func TestRunnerReserveIdempotencyRetriesStaleConflict(t *testing.T) {
	store := &staleReservationStore{MemoryStore: NewMemoryStore()}
	runner := NewRunner(&RunnerOptions{
		Store:       store,
		IDGenerator: func() string { return "op_retry_reservation" },
	})
	result, err := runner.Execute(context.Background(), &Spec{
		Type:           "task.create",
		Mode:           ModeTracked,
		IdempotencyKey: "request-stale",
	}, func(ctx context.Context) (*Result, error) {
		return &Result{}, nil
	})
	if err != nil {
		t.Fatalf("expected stale reservation retry to succeed: %v", err)
	}
	if result.Operation.ID != "op_retry_reservation" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if store.reserveCalls != 2 {
		t.Fatalf("expected one retry, got %d calls", store.reserveCalls)
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

func TestRunnerExecuteRejectsInlineIdempotencyKey(t *testing.T) {
	runner := NewRunner(nil)

	called := false
	_, err := runner.Execute(context.Background(), &Spec{
		Type:           "task.create",
		Mode:           ModeInline,
		IdempotencyKey: "request-inline",
	}, func(ctx context.Context) (*Result, error) {
		called = true
		return &Result{}, nil
	})
	if err == nil {
		t.Fatalf("expected inline idempotency key to be rejected")
	}
	if called {
		t.Fatalf("handler should not run for invalid spec")
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

func TestRunnerBeginIdempotencyCallRejectsCancelledOwner(t *testing.T) {
	runner := NewRunner(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	call, owner, err := runner.beginIdempotencyCall(ctx, "request-cancelled")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if call != nil || owner {
		t.Fatalf("cancelled context should not register owner, got call=%v owner=%v", call, owner)
	}
	runner.idempotencyMu.Lock()
	inflight := runner.idempotency.Len()
	runner.idempotencyMu.Unlock()
	if inflight != 0 {
		t.Fatalf("cancelled owner leaked inflight call: %d", inflight)
	}
}

func TestRunnerFinishIdempotencyCallStoresOperationResultOnError(t *testing.T) {
	runner := NewRunner(nil)
	call, owner, err := runner.beginIdempotencyCall(context.Background(), "request-error")
	if err != nil {
		t.Fatalf("begin idempotency call: %v", err)
	}
	if !owner {
		t.Fatal("expected owner")
	}

	runner.finishIdempotencyCall("request-error", call, &Result{
		Operation: Operation{ID: "op_error", Type: "task.test", Mode: ModeTracked, Status: StatusAccepted},
	}, errors.New("business error"))

	result, waitErr := waitRunnerIdempotencyCall(context.Background(), call)
	if result == nil || result.Operation.ID != "op_error" {
		t.Fatalf("failed call should retain operation result, got %+v", result)
	}
	if waitErr == nil {
		t.Fatal("expected failed call error to be retained for waiters")
	}
}

func TestMemoryStorePutWithIdempotencyKeyRebindsMissingBoundOperation(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	first := &Result{Operation: Operation{ID: "op_first", Type: "task.create", Mode: ModeTracked, Status: StatusAccepted}}
	if err := store.PutWithIdempotencyKey(ctx, "request-1", first); err != nil {
		t.Fatalf("put first: %v", err)
	}
	store.mu.Lock()
	delete(store.records, "op_first")
	store.mu.Unlock()

	second := &Result{Operation: Operation{ID: "op_second", Type: "task.create", Mode: ModeTracked, Status: StatusAccepted}}
	if err := store.PutWithIdempotencyKey(ctx, "request-1", second); err != nil {
		t.Fatalf("put second after missing binding: %v", err)
	}
	got, err := store.GetByIdempotencyKey(ctx, "request-1")
	if err != nil {
		t.Fatalf("get rebound key: %v", err)
	}
	if got.Operation.ID != "op_second" {
		t.Fatalf("expected rebound operation, got %q", got.Operation.ID)
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

func TestRunner_IdempotencySingleflight_WaiterOnOwnerErrorAndPanic(t *testing.T) {
	runner := NewRunner(nil)
	ctx := context.Background()
	specErr := &Spec{Type: "task.test", Mode: ModeTracked, IdempotencyKey: "runner-key-error"}
	specPanic := &Spec{Type: "task.test", Mode: ModeTracked, IdempotencyKey: "runner-key-panic"}

	// 1. 测试 owner 返回 error 的情况
	t.Run("owner returns error", func(t *testing.T) {
		ownerResultErr := errors.New("business error")

		call, owner, err := runner.beginIdempotencyCall(ctx, specErr.IdempotencyKey)
		if err != nil {
			t.Fatalf("begin idempotency call: %v", err)
		}
		if !owner {
			t.Fatal("expected test call to own idempotency key")
		}

		var res *Result
		var waiterErr error
		waiterDone := make(chan struct{})
		go func() {
			defer close(waiterDone)
			res, waiterErr = waitRunnerIdempotencyCall(ctx, call)
		}()

		runner.finishIdempotencyCall(specErr.IdempotencyKey, call, nil, ownerResultErr)
		waitForSignal(t, waiterDone, "timed out waiting for idempotency waiter")

		if res != nil {
			t.Fatalf("waiter expected nil result, got %+v", res)
		}
		if waiterErr == nil || waiterErr.Error() != ownerResultErr.Error() {
			t.Fatalf("waiter expected owner error %v, got %v", ownerResultErr, waiterErr)
		}
	})

	// 2. 测试 owner panic 的情况
	t.Run("owner panics", func(t *testing.T) {
		call, owner, err := runner.beginIdempotencyCall(ctx, specPanic.IdempotencyKey)
		if err != nil {
			t.Fatalf("begin idempotency call: %v", err)
		}
		if !owner {
			t.Fatal("expected test call to own idempotency key")
		}

		var res *Result
		var waiterErr error
		waiterDone := make(chan struct{})
		go func() {
			defer close(waiterDone)
			res, waiterErr = waitRunnerIdempotencyCall(ctx, call)
		}()

		runner.finishIdempotencyCall(specPanic.IdempotencyKey, call, nil, idempotencyPanicError("boom"))
		waitForSignal(t, waiterDone, "timed out waiting for idempotency waiter")

		if res != nil {
			t.Fatalf("waiter expected nil result, got %+v", res)
		}
		if waiterErr == nil || !strings.Contains(waiterErr.Error(), "idempotent operation panicked: boom") {
			t.Fatalf("waiter expected panic error with original value, got %v", waiterErr)
		}
	})
}

func waitForSignal(t *testing.T, ch <-chan struct{}, timeoutMessage string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal(timeoutMessage)
	}
}

func TestSpecValidateRejectsInvalidIdempotencyKey(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{name: "leading space", key: " request-1"},
		{name: "embedded space", key: "request 1"},
		{name: "control", key: "request\n1"},
		{name: "too long", key: strings.Repeat("a", maxIdempotencyKeyLength+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (&Spec{Type: "task.complete", Mode: ModeTracked, IdempotencyKey: tc.key}).Validate()
			if err == nil {
				t.Fatalf("expected invalid idempotency key to be rejected")
			}
		})
	}
}

func TestSpecValidateAcceptsBoundedIdempotencyKey(t *testing.T) {
	err := (&Spec{Type: "task.complete", Mode: ModeTracked, IdempotencyKey: strings.Repeat("a", maxIdempotencyKeyLength)}).Validate()
	if err != nil {
		t.Fatalf("expected max-length idempotency key to be accepted: %v", err)
	}
}
