package writeflow

import (
	"context"
	"testing"

	"gochen/contextx"
	"gochen/errors"
)

// fakeTx 模拟真实事务执行器：fn 成功后在提交后排空 after-commit 回调。
type fakeTx struct {
	beginCalls int
}

func (f *fakeTx) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	f.beginCalls++
	txCtx, err := contextx.WithTxLifecycle(ctx, true)
	if err != nil {
		return err
	}
	if err := fn(txCtx); err != nil {
		return err
	}
	return contextx.RunAfterCommit(txCtx)
}

func inTx(ctx context.Context) bool {
	_, ok := contextx.TxLifecycleFromContext(ctx)
	return ok
}

func TestRun_ExecutesStagesInOrderAndPassesPayload(t *testing.T) {
	tx := &fakeTx{}
	var order []string
	var gotPayload int

	err := New[int](tx).
		Action(func(context.Context, int) error {
			order = append(order, "action")
			return nil
		}).
		Before(func(context.Context, int) error {
			order = append(order, "before")
			return nil
		}).
		Before(func(context.Context, int) error {
			order = append(order, "before2")
			return nil
		}).
		Validate(func(int) error {
			order = append(order, "validate")
			return nil
		}).
		After(func(_ context.Context, payload int) error {
			order = append(order, "after")
			gotPayload = payload
			return nil
		}).
		PostCommit(func(context.Context, int) error {
			order = append(order, "post-commit")
			return nil
		}).
		Run(context.Background(), 7)

	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := []string{"before", "before2", "validate", "action", "after", "post-commit"}
	if len(order) != len(want) {
		t.Fatalf("stage order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("stage order = %v, want %v", order, want)
		}
	}
	if gotPayload != 7 {
		t.Fatalf("payload = %d, want 7", gotPayload)
	}
	if tx.beginCalls != 1 {
		t.Fatalf("beginCalls = %d, want 1", tx.beginCalls)
	}
}

func TestRun_RunsStagesInsideTxAndPostCommitWithCallerCtx(t *testing.T) {
	tx := &fakeTx{}
	var actionInTx, afterInTx, postCommitInTx bool

	err := New[int](tx).
		Action(func(ctx context.Context, _ int) error {
			actionInTx = inTx(ctx)
			return nil
		}).
		After(func(ctx context.Context, _ int) error {
			afterInTx = inTx(ctx)
			return nil
		}).
		PostCommit(func(ctx context.Context, _ int) error {
			postCommitInTx = inTx(ctx)
			return nil
		}).
		Run(context.Background(), 1)

	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !actionInTx || !afterInTx {
		t.Fatalf("expected action/after inside tx, got action=%v after=%v", actionInTx, afterInTx)
	}
	if postCommitInTx {
		t.Fatal("post-commit callback must receive caller ctx without tx lifecycle")
	}
}

func TestRun_OutsideTxRunsPreflightOutsideTransaction(t *testing.T) {
	tx := &fakeTx{}
	var beforeInTx, actionInTx bool
	var validateCalled bool

	err := New[int](tx).
		Action(func(ctx context.Context, _ int) error {
			actionInTx = inTx(ctx)
			return nil
		}).
		Before(func(ctx context.Context, _ int) error {
			beforeInTx = inTx(ctx)
			return nil
		}).
		Validate(func(int) error {
			validateCalled = true
			return nil
		}).
		OutsideTx().
		Run(context.Background(), 1)

	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if beforeInTx {
		t.Fatal("expected outside-tx preflight to run outside transaction")
	}
	if !validateCalled {
		t.Fatal("expected validate to run")
	}
	if !actionInTx {
		t.Fatal("expected action to run inside transaction")
	}
}

func TestRun_PreflightFailureSkipsTransaction(t *testing.T) {
	tx := &fakeTx{}
	want := errors.New("before failed")
	var actionCalled bool

	err := New[int](tx).
		Action(func(context.Context, int) error {
			actionCalled = true
			return nil
		}).
		Before(func(context.Context, int) error { return want }).
		Run(context.Background(), 1)

	if !errors.Is(err, want) {
		t.Fatalf("expected before error, got %v", err)
	}
	if actionCalled {
		t.Fatal("expected action to be skipped")
	}
	if tx.beginCalls != 1 {
		t.Fatalf("beginCalls = %d, want 1 (preflight runs inside tx and rolls back)", tx.beginCalls)
	}
}

func TestRun_AfterFailureAbortsBeforePostCommit(t *testing.T) {
	tx := &fakeTx{}
	want := errors.New("after failed")
	var postCommitCalled bool

	err := New[int](tx).
		Action(func(context.Context, int) error { return nil }).
		After(func(context.Context, int) error { return want }).
		PostCommit(func(context.Context, int) error {
			postCommitCalled = true
			return nil
		}).
		Run(context.Background(), 1)

	if !errors.Is(err, want) {
		t.Fatalf("expected after error, got %v", err)
	}
	if postCommitCalled {
		t.Fatal("expected post-commit to be skipped on after failure")
	}
}

func TestRun_RequiresAction(t *testing.T) {
	err := New[int](nil).
		Before(func(context.Context, int) error { return nil }).
		Run(context.Background(), 1)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

func TestRun_NilCtxIsInvalidInput(t *testing.T) {
	err := New[int](nil).
		Action(func(context.Context, int) error { return nil }).
		Run(nil, 1)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

func TestRun_NoTxPostCommitFailsBeforePreflight(t *testing.T) {
	var beforeCalled bool

	err := New[int](nil).
		Action(func(context.Context, int) error { return nil }).
		Before(func(context.Context, int) error {
			beforeCalled = true
			return nil
		}).
		PostCommit(func(context.Context, int) error { return nil }).
		OutsideTx().
		Run(context.Background(), 1)

	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("expected Unsupported, got %v", err)
	}
	if beforeCalled {
		t.Fatal("expected preflight to be skipped on Unsupported")
	}
}

func TestRun_NoTxOutsidePreflightStillRunsWithoutPostCommit(t *testing.T) {
	var order []string

	err := New[int](nil).
		Action(func(context.Context, int) error {
			order = append(order, "action")
			return nil
		}).
		Before(func(context.Context, int) error {
			order = append(order, "before")
			return nil
		}).
		Validate(func(int) error {
			order = append(order, "validate")
			return nil
		}).
		OutsideTx().
		Run(context.Background(), 1)

	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(order) != 3 || order[0] != "before" || order[1] != "validate" || order[2] != "action" {
		t.Fatalf("stages = %v", order)
	}
}

func TestValidateBatchSizeBoundaries(t *testing.T) {
	if err := ValidateBatchSize(3, 3); err != nil {
		t.Fatalf("expected boundary size to pass: %v", err)
	}
	if err := ValidateBatchSize(4, 3); !errors.Is(err, errors.Validation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestEachRunsInOrderAndStopsOnError(t *testing.T) {
	var got []int
	step := Each(func(_ context.Context, item int) error {
		got = append(got, item)
		if item == 2 {
			return errors.New("stop")
		}
		return nil
	})

	err := step(context.Background(), []int{1, 2, 3})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("items = %v, want [1 2]", got)
	}
}

func TestEachPureStopsOnError(t *testing.T) {
	var got []int
	step := EachPure(func(item int) error {
		got = append(got, item)
		if item == 2 {
			return errors.New("stop")
		}
		return nil
	})

	err := step([]int{1, 2, 3})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(got) != 2 {
		t.Fatalf("items = %v, want [1 2]", got)
	}
}

func TestEachAllRunsAllAndJoinsErrors(t *testing.T) {
	evenErr := errors.New("even failed")
	lastErr := errors.New("last failed")
	var got []int

	step := EachAll(func(_ context.Context, item int) error {
		got = append(got, item)
		if item == 2 {
			return evenErr
		}
		if item == 4 {
			return lastErr
		}
		return nil
	})

	err := step(context.Background(), []int{1, 2, 3, 4})
	if !errors.Is(err, evenErr) || !errors.Is(err, lastErr) {
		t.Fatalf("expected joined error containing both failures, got %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected all items to run, got %v", got)
	}
}

func TestLiftersReturnNilForNilStep(t *testing.T) {
	if Each[int](nil) != nil {
		t.Fatal("Each(nil) must return nil")
	}
	if EachPure[int](nil) != nil {
		t.Fatal("EachPure(nil) must return nil")
	}
	if EachAll[int](nil) != nil {
		t.Fatal("EachAll(nil) must return nil")
	}
}

func TestJoinSkipsNilAndStopsOnError(t *testing.T) {
	want := errors.New("boom")
	var order []string

	joined := Join(
		nil,
		func(context.Context, int) error {
			order = append(order, "a")
			return nil
		},
		func(context.Context, int) error {
			order = append(order, "b")
			return want
		},
		func(context.Context, int) error {
			order = append(order, "c")
			return nil
		},
	)
	if joined == nil {
		t.Fatal("expected joined step")
	}

	err := joined(context.Background(), 1)
	if !errors.Is(err, want) {
		t.Fatalf("expected joined error, got %v", err)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("order = %v, want [a b]", order)
	}
}

func TestJoinSingleReturnsOriginalStep(t *testing.T) {
	want := errors.New("boom")
	joined := Join(func(context.Context, int) error { return want })
	if joined == nil {
		t.Fatal("expected joined step")
	}
	if err := joined(context.Background(), 1); !errors.Is(err, want) {
		t.Fatalf("expected original step error, got %v", err)
	}
}

func TestJoinAllNilReturnsNil(t *testing.T) {
	if Join[int](nil, nil) != nil {
		t.Fatal("Join(nil...) must return nil")
	}
}

func TestRun_CancelledContextReturnsImmediateError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	hookCalled := false
	actionCalled := false

	err := New[int](nil).
		Before(func(context.Context, int) error {
			hookCalled = true
			return nil
		}).
		Action(func(context.Context, int) error {
			actionCalled = true
			return nil
		}).
		Run(ctx, 42)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if hookCalled {
		t.Fatal("expected Before hook NOT to be called on cancelled context")
	}
	if actionCalled {
		t.Fatal("expected Action NOT to be called on cancelled context")
	}
}
