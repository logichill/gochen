package operation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gochen/contextx"
	gerrors "gochen/errors"
)

func TestRunnerReservationAfterRequestCancellation(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "handler_success"},
		{name: "handler_error", err: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryStore()
			runner := NewRunner(&RunnerOptions{Store: store})
			spec := &Spec{Type: "task.create", Mode: ModeTracked, IdempotencyKey: "canceled-request"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := runner.Execute(ctx, spec, func(context.Context) (*Result, error) {
				cancel()
				return nil, tc.err
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected canceled result persistence, got %v", err)
			}
			// 新 Runner 验证业务失败可重试，业务成功后结果丢失则保留预占。
			retry := NewRunner(&RunnerOptions{Store: store})
			called := false
			_, err = retry.Execute(context.Background(), spec, func(context.Context) (*Result, error) {
				called = true
				return nil, nil
			})
			if tc.err == nil {
				if called || !gerrors.Is(err, gerrors.Conflict) {
					t.Fatalf("completed handler must not run again: called=%v err=%v", called, err)
				}
			} else if err != nil || !called {
				t.Fatalf("failed handler reservation was not released: called=%v err=%v", called, err)
			}
		})
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

func TestRunnerExecuteTracked_PreservesReservationWhenSuccessfulResultCannotBeStored(t *testing.T) {
	ctx := context.Background()
	store := newFailOnceOnPutStore()
	runner := NewRunner(&RunnerOptions{Store: store})
	spec := &Spec{Type: "task.complete", Mode: ModeTracked, IdempotencyKey: "request-store-failure"}
	calls := 0
	handler := func(context.Context) (*Result, error) {
		calls++
		return &Result{Result: map[string]any{"done": true}}, nil
	}
	result, err := runner.Execute(ctx, spec, handler)
	if !contextx.IsAfterCommitError(err) || !gerrors.Is(err, gerrors.Database) {
		t.Fatalf("expected committed store failure: %v", err)
	}
	if result == nil || result.Operation.ID == "" {
		t.Fatal("missing operation identity for reconciliation")
	}
	// 使用另一个 Runner，证明保护来自存储预占，而非单实例 singleflight。
	other := NewRunner(&RunnerOptions{Store: store})
	if _, err := other.Execute(ctx, spec, handler); !gerrors.Is(err, gerrors.Conflict) {
		t.Fatalf("expected pending reservation: %v", err)
	}
	if calls != 1 {
		t.Fatalf("business executed %d times", calls)
	}
	if err := store.PutWithIdempotencyKey(ctx, spec.IdempotencyKey, result); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	replayed, err := other.Execute(ctx, spec, handler)
	if err != nil || replayed.Operation.ID != result.Operation.ID || calls != 1 {
		t.Fatalf("replay after reconciliation: result=%+v calls=%d err=%v", replayed, calls, err)
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
