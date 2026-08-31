package singleflight

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRegistryBeginFinishWait(t *testing.T) {
	var mu sync.Mutex
	reg := NewRegistry[string, string]()

	mu.Lock()
	ownerCall, owner, err := reg.Begin(context.Background(), "key")
	if err != nil {
		t.Fatalf("begin owner: %v", err)
	}
	waiterCall, waiterOwner, err := reg.Begin(context.Background(), "key")
	mu.Unlock()
	if err != nil {
		t.Fatalf("begin waiter: %v", err)
	}
	if !owner || waiterOwner || ownerCall != waiterCall {
		t.Fatalf("unexpected ownership: owner=%v waiterOwner=%v same=%v", owner, waiterOwner, ownerCall == waiterCall)
	}

	waitDone := make(chan string, 1)
	go func() {
		got, waitErr := waiterCall.Wait(context.Background(), WaitOptions[string]{})
		if waitErr != nil {
			t.Errorf("wait returned error: %v", waitErr)
			return
		}
		waitDone <- got
	}()

	mu.Lock()
	if ok := reg.Finish("key", ownerCall, "ok", nil, FinishOptions[string]{}); !ok {
		t.Fatal("expected finish to close owner call")
	}
	inflight := reg.Len()
	mu.Unlock()
	if inflight != 0 {
		t.Fatalf("expected registry to be empty, got %d", inflight)
	}
	if got := <-waitDone; got != "ok" {
		t.Fatalf("waiter got %q, want ok", got)
	}
}

func TestFinishKeepResultFalse_WaiterGetsNoResultError(t *testing.T) {
	var mu sync.Mutex
	reg := NewRegistry[string, string]()

	mu.Lock()
	ownerCall, owner, err := reg.Begin(context.Background(), "key")
	if err != nil {
		t.Fatalf("begin owner: %v", err)
	}
	waiterCall, waiterOwner, err := reg.Begin(context.Background(), "key")
	mu.Unlock()
	if err != nil {
		t.Fatalf("begin waiter: %v", err)
	}
	if !owner || waiterOwner || ownerCall != waiterCall {
		t.Fatalf("unexpected ownership: owner=%v waiterOwner=%v same=%v", owner, waiterOwner, ownerCall == waiterCall)
	}

	noResultErr := errors.New("operation discarded")
	waitDone := make(chan error, 1)
	go func() {
		_, waitErr := waiterCall.Wait(context.Background(), WaitOptions[string]{
			HasResult:     func(result string) bool { return result != "" },
			NoResultError: func() error { return noResultErr },
		})
		waitDone <- waitErr
	}()

	mu.Lock()
	if ok := reg.Finish("key", ownerCall, "", nil, FinishOptions[string]{
		KeepResult: func(result string) bool { return result != "" },
	}); !ok {
		t.Fatal("expected finish to close owner call")
	}
	mu.Unlock()

	gotErr := <-waitDone
	if gotErr != noResultErr {
		t.Fatalf("waiter expected NoResultError, got %v", gotErr)
	}
}

func TestFinishKeepResultFalse_WithoutNoResultError_WaiterGetsZeroValue(t *testing.T) {
	var mu sync.Mutex
	reg := NewRegistry[string, string]()

	mu.Lock()
	ownerCall, owner, err := reg.Begin(context.Background(), "key")
	if err != nil {
		t.Fatalf("begin owner: %v", err)
	}
	waiterCall, waiterOwner, err := reg.Begin(context.Background(), "key")
	mu.Unlock()
	if err != nil {
		t.Fatalf("begin waiter: %v", err)
	}
	if !owner || waiterOwner || ownerCall != waiterCall {
		t.Fatalf("unexpected ownership: owner=%v waiterOwner=%v same=%v", owner, waiterOwner, ownerCall == waiterCall)
	}

	waitDone := make(chan string, 1)
	go func() {
		got, waitErr := waiterCall.Wait(context.Background(), WaitOptions[string]{})
		if waitErr != nil {
			t.Errorf("wait returned error: %v", waitErr)
			return
		}
		waitDone <- got
	}()

	mu.Lock()
	if ok := reg.Finish("key", ownerCall, "", nil, FinishOptions[string]{
		KeepResult: func(result string) bool { return result != "" },
	}); !ok {
		t.Fatal("expected finish to close owner call")
	}
	mu.Unlock()

	got := <-waitDone
	if got != "" {
		t.Fatalf("waiter got %q, want empty string (zero value)", got)
	}
}

func TestFinishClonePanicStillWakesWaiters(t *testing.T) {
	reg := NewRegistry[string, string]()
	ownerCall, owner, err := reg.Begin(context.Background(), "key")
	if err != nil {
		t.Fatalf("begin owner: %v", err)
	}
	waiterCall, waiterOwner, err := reg.Begin(context.Background(), "key")
	if err != nil {
		t.Fatalf("begin waiter: %v", err)
	}
	if !owner || waiterOwner || ownerCall != waiterCall {
		t.Fatalf("unexpected ownership: owner=%v waiterOwner=%v same=%v", owner, waiterOwner, ownerCall == waiterCall)
	}

	waitDone := make(chan error, 1)
	go func() {
		_, waitErr := waiterCall.Wait(context.Background(), WaitOptions[string]{})
		waitDone <- waitErr
	}()

	if ok := reg.Finish("key", ownerCall, "ok", nil, FinishOptions[string]{
		Clone: func(string) string { panic("clone panic") },
	}); !ok {
		t.Fatal("expected finish to close owner call")
	}
	if reg.Len() != 0 {
		t.Fatalf("expected registry to be empty, got %d", reg.Len())
	}

	select {
	case waitErr := <-waitDone:
		if waitErr == nil || !strings.Contains(waitErr.Error(), "clone panic") {
			t.Fatalf("waiter expected finish panic error, got %v", waitErr)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter was not woken after finish panic")
	}
}

func TestRunFinishesWaitersBeforeRepanic(t *testing.T) {
	var mu sync.Mutex
	reg := NewRegistry[string, string]()

	mu.Lock()
	ownerCall, owner, err := reg.Begin(context.Background(), "key")
	if err != nil {
		t.Fatalf("begin owner: %v", err)
	}
	waiterCall, waiterOwner, err := reg.Begin(context.Background(), "key")
	mu.Unlock()
	if err != nil {
		t.Fatalf("begin waiter: %v", err)
	}
	if !owner || waiterOwner {
		t.Fatalf("unexpected ownership: owner=%v waiterOwner=%v", owner, waiterOwner)
	}

	waitErr := make(chan error, 1)
	go func() {
		_, err := waiterCall.Wait(context.Background(), WaitOptions[string]{})
		waitErr <- err
	}()

	func() {
		defer func() {
			if recovered := recover(); recovered != "boom" {
				t.Fatalf("expected original panic, got %v", recovered)
			}
		}()
		_, _ = Run(context.Background(), ownerCall, owner,
			func(ctx context.Context, call *Call[string]) (string, error) {
				return call.Wait(ctx, WaitOptions[string]{})
			},
			func(call *Call[string], result string, err error) {
				mu.Lock()
				defer mu.Unlock()
				reg.Finish("key", call, result, err, FinishOptions[string]{})
			},
			func(recovered any) error {
				return errors.New("panic: " + recovered.(string))
			},
			func() (string, error) {
				panic("boom")
			},
		)
	}()

	err = <-waitErr
	if err == nil || !strings.Contains(err.Error(), "panic: boom") {
		t.Fatalf("waiter expected panic error, got %v", err)
	}
}

func TestRunFinishPanicDoesNotReplaceFunctionPanic(t *testing.T) {
	ownerCall := &Call[string]{done: make(chan struct{})}

	func() {
		defer func() {
			if recovered := recover(); recovered != "owner panic" {
				t.Fatalf("expected owner panic, got %v", recovered)
			}
		}()
		_, _ = Run(context.Background(), ownerCall, true,
			nil,
			func(*Call[string], string, error) {
				panic("finish panic")
			},
			func(recovered any) error {
				return errors.New("panic: " + recovered.(string))
			},
			func() (string, error) {
				panic("owner panic")
			},
		)
	}()
}

func TestRunFinishPanicWakesWaiter(t *testing.T) {
	ownerCall := &Call[string]{done: make(chan struct{})}

	waitErr := make(chan error, 1)
	go func() {
		_, err := ownerCall.Wait(context.Background(), WaitOptions[string]{})
		waitErr <- err
	}()

	got, err := Run(context.Background(), ownerCall, true,
		nil,
		func(*Call[string], string, error) {
			panic("finish panic")
		},
		nil,
		func() (string, error) {
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("got %q, want ok", got)
	}

	select {
	case err := <-waitErr:
		if err == nil || !strings.Contains(err.Error(), "finish panic") {
			t.Fatalf("waiter expected finish panic error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter was not woken after finish panic")
	}
}

func TestRegistryBeginIgnoresClosedInflightCall(t *testing.T) {
	reg := NewRegistry[string, string]()
	ownerCall, owner, err := reg.Begin(context.Background(), "key")
	if err != nil {
		t.Fatalf("begin owner: %v", err)
	}
	if !owner {
		t.Fatal("expected first begin to own call")
	}
	ownerCall.closed.Store(true)
	close(ownerCall.done)

	nextCall, nextOwner, err := reg.Begin(context.Background(), "key")
	if err != nil {
		t.Fatalf("begin after closed call: %v", err)
	}
	if !nextOwner {
		t.Fatal("expected closed inflight call to be discarded")
	}
	if nextCall == ownerCall {
		t.Fatal("expected a new call after closed inflight call")
	}
	if reg.Len() != 1 {
		t.Fatalf("expected one active inflight call, got %d", reg.Len())
	}
}
