package contextx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAfterCommitDispatcher_RunAfterCommitJoinsErrorsAndRunsAllCallbacks(t *testing.T) {
	firstErr := errors.New("first callback failed")
	secondErr := errors.New("second callback failed")
	secondCalled := false
	dispatcher := NewAfterCommitDispatcher()
	ctx := context.Background()

	if err := dispatcher.AppendAfterCommit(ctx, func(context.Context) error { return firstErr }); err != nil {
		t.Fatalf("append first callback: %v", err)
	}
	if err := dispatcher.AppendAfterCommit(ctx, func(context.Context) error {
		secondCalled = true
		return secondErr
	}); err != nil {
		t.Fatalf("append second callback: %v", err)
	}

	err := dispatcher.RunAfterCommit()
	if !secondCalled {
		t.Fatal("expected second callback to run after first callback failed")
	}
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("expected joined callback errors, got %v", err)
	}
}

func TestRunAfterCommitJoinsErrorsAndRunsAllCallbacks(t *testing.T) {
	firstErr := errors.New("first callback failed")
	secondErr := errors.New("second callback failed")
	secondCalled := false
	ctx, err := WithTxLifecycle(context.Background(), true)
	if err != nil {
		t.Fatalf("WithTxLifecycle returned error: %v", err)
	}
	if err := AppendAfterCommit(ctx, func(context.Context) error { return firstErr }); err != nil {
		t.Fatalf("append first callback: %v", err)
	}
	if err := AppendAfterCommit(ctx, func(context.Context) error {
		secondCalled = true
		return secondErr
	}); err != nil {
		t.Fatalf("append second callback: %v", err)
	}

	err = RunAfterCommit(ctx)
	if !secondCalled {
		t.Fatal("expected second callback to run after first callback failed")
	}
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("expected joined callback errors, got %v", err)
	}
}

func TestAfterCommitDispatcher_RunAfterCommitSkipsNilCallbacks(t *testing.T) {
	dispatcher := &AfterCommitDispatcher{
		entries: []afterCommitEntry{
			{ctx: context.Background(), fn: nil},
			{ctx: context.Background(), fn: func(ctx context.Context) error { return nil }},
		},
	}

	if err := dispatcher.RunAfterCommit(); err != nil {
		t.Fatalf("RunAfterCommit returned error: %v", err)
	}
	if !dispatcher.drained {
		t.Fatalf("expected dispatcher to be drained")
	}
}

func TestRunAfterCommitSkipsNilCallbacks(t *testing.T) {
	ctx, err := WithTxLifecycle(context.Background(), true)
	if err != nil {
		t.Fatalf("WithTxLifecycle returned error: %v", err)
	}
	state, _, ok := txLifecycleStateFromContext(ctx)
	if !ok {
		t.Fatalf("expected tx lifecycle state")
	}
	called := false
	state.callbacks = []AfterCommitFunc{
		nil,
		func(ctx context.Context) error {
			called = true
			return nil
		},
	}

	if err := RunAfterCommit(ctx); err != nil {
		t.Fatalf("RunAfterCommit returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected callback after nil callback to run")
	}
}

func TestAfterCommitDispatcher_ConcurrentAppendAndRun(t *testing.T) {
	dispatcher := NewAfterCommitDispatcher()
	ctx := context.Background()

	var calls atomic.Int64
	const workers = 64
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = dispatcher.AppendAfterCommit(ctx, func(ctx context.Context) error {
				calls.Add(1)
				return nil
			})
		}()
	}

	runErr := make(chan error, 1)
	go func() {
		<-start
		runErr <- dispatcher.RunAfterCommit()
	}()

	close(start)
	wg.Wait()
	if err := <-runErr; err != nil {
		t.Fatalf("RunAfterCommit returned error: %v", err)
	}
	if err := dispatcher.RunAfterCommit(); err != nil {
		t.Fatalf("second RunAfterCommit returned error: %v", err)
	}
	if err := dispatcher.AppendAfterCommit(ctx, func(ctx context.Context) error { return nil }); err == nil {
		t.Fatalf("expected AppendAfterCommit after drain to fail")
	}
	_ = calls.Load()
}

func TestAppendAfterCommit_ConcurrentAppendAndRun(t *testing.T) {
	ctx, err := WithTxLifecycle(context.Background(), true)
	if err != nil {
		t.Fatalf("WithTxLifecycle returned error: %v", err)
	}

	var calls atomic.Int64
	const workers = 64
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = AppendAfterCommit(ctx, func(ctx context.Context) error {
				calls.Add(1)
				return nil
			})
		}()
	}

	runErr := make(chan error, 1)
	go func() {
		<-start
		runErr <- RunAfterCommit(ctx)
	}()

	close(start)
	wg.Wait()
	if err := <-runErr; err != nil {
		t.Fatalf("RunAfterCommit returned error: %v", err)
	}
	if err := RunAfterCommit(ctx); err != nil {
		t.Fatalf("second RunAfterCommit returned error: %v", err)
	}
	if err := AppendAfterCommit(ctx, func(ctx context.Context) error { return nil }); err == nil {
		t.Fatalf("expected AppendAfterCommit after drain to fail")
	}
	_ = calls.Load()
}

// 嵌套内层回滚只应撤销内层注册的回调，外层在进入内层前注册的回调必须保留。
func TestResetAfterCommitInnerRollbackKeepsOuterCallbacks(t *testing.T) {
	ctx := context.Background()

	outerCtx, err := WithTxLifecycle(ctx, true)
	if err != nil {
		t.Fatalf("outer lifecycle: %v", err)
	}
	outerCalled := false
	if err := AppendAfterCommit(outerCtx, func(context.Context) error {
		outerCalled = true
		return nil
	}); err != nil {
		t.Fatalf("append outer callback: %v", err)
	}

	innerCtx, err := WithTxLifecycle(outerCtx, false)
	if err != nil {
		t.Fatalf("inner lifecycle: %v", err)
	}
	innerCalled := false
	if err := AppendAfterCommit(innerCtx, func(context.Context) error {
		innerCalled = true
		return nil
	}); err != nil {
		t.Fatalf("append inner callback: %v", err)
	}

	// 内层回滚：只撤销内层回调。
	ResetAfterCommit(innerCtx)

	// 外层提交：只应执行外层回调。
	if err := RunAfterCommit(outerCtx); err != nil {
		t.Fatalf("outer run after commit: %v", err)
	}
	if innerCalled {
		t.Fatal("inner callback must be revoked by inner rollback")
	}
	if !outerCalled {
		t.Fatal("outer callback must survive inner rollback")
	}
}

// 最外层回滚（owned=true）封存整个事务，后续追加必须失败。
func TestResetAfterCommitOuterRollbackSealsState(t *testing.T) {
	ctx := context.Background()
	outerCtx, err := WithTxLifecycle(ctx, true)
	if err != nil {
		t.Fatalf("outer lifecycle: %v", err)
	}
	if err := AppendAfterCommit(outerCtx, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("append: %v", err)
	}
	ResetAfterCommit(outerCtx)
	if err := AppendAfterCommit(outerCtx, func(context.Context) error { return nil }); err == nil {
		t.Fatal("append after outer rollback must fail")
	}
	if err := RunAfterCommit(outerCtx); err != nil {
		t.Fatalf("run after commit on sealed state: %v", err)
	}
}

// 注入分发器时，嵌套内层回滚必须按水位线截断分发器中的内层回调。
func TestResetAfterCommitInnerRollbackTruncatesDispatcher(t *testing.T) {
	dispatcher := NewAfterCommitDispatcher()
	base := context.Background()
	base, err := WithAfterCommitDispatcher(base, dispatcher)
	if err != nil {
		t.Fatalf("with dispatcher: %v", err)
	}

	outerCtx, err := WithTxLifecycle(base, true)
	if err != nil {
		t.Fatalf("outer lifecycle: %v", err)
	}
	outerCalled := false
	if err := AppendAfterCommit(outerCtx, func(context.Context) error {
		outerCalled = true
		return nil
	}); err != nil {
		t.Fatalf("append outer: %v", err)
	}

	innerCtx, err := WithTxLifecycle(outerCtx, false)
	if err != nil {
		t.Fatalf("inner lifecycle: %v", err)
	}
	innerCalled := false
	if err := AppendAfterCommit(innerCtx, func(context.Context) error {
		innerCalled = true
		return nil
	}); err != nil {
		t.Fatalf("append inner: %v", err)
	}

	ResetAfterCommit(innerCtx)

	if err := RunAfterCommit(outerCtx); err != nil {
		t.Fatalf("run after commit: %v", err)
	}
	if innerCalled {
		t.Fatal("inner callback in dispatcher must be truncated by inner rollback")
	}
	if !outerCalled {
		t.Fatal("outer callback in dispatcher must survive inner rollback")
	}
}

// 最外层回滚时，注入的分发器必须被整体 Reset，且可复用。
func TestResetAfterCommitOuterRollbackResetsDispatcher(t *testing.T) {
	dispatcher := NewAfterCommitDispatcher()
	base := context.Background()
	base, err := WithAfterCommitDispatcher(base, dispatcher)
	if err != nil {
		t.Fatalf("with dispatcher: %v", err)
	}
	outerCtx, err := WithTxLifecycle(base, true)
	if err != nil {
		t.Fatalf("outer lifecycle: %v", err)
	}
	called := false
	if err := AppendAfterCommit(outerCtx, func(context.Context) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	ResetAfterCommit(outerCtx)
	if err := RunAfterCommit(outerCtx); err != nil {
		t.Fatalf("run: %v", err)
	}
	if called {
		t.Fatal("callback must not run after outer rollback")
	}
	// 分发器复位后可再次注册并执行。
	if err := dispatcher.AppendAfterCommit(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("dispatcher must be reusable after reset: %v", err)
	}
}

func TestWithAfterCommitMark_RejectsNilContextAndRecordsMark(t *testing.T) {
	if got, err := WithAfterCommitMark(nil, 3); err == nil || got != nil {
		t.Fatalf("expected nil context error, got ctx=%v err=%v", got, err)
	}

	ctx, err := WithAfterCommitMark(context.Background(), 5)
	if err != nil {
		t.Fatalf("WithAfterCommitMark returned error: %v", err)
	}
	if mark, ok := dispatcherMarkFromContext(ctx); !ok || mark != 5 {
		t.Fatalf("expected mark=5, got mark=%d ok=%v", mark, ok)
	}
}
