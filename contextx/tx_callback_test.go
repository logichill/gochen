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
