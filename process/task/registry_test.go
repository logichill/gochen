package task

import (
	"context"
	"sync/atomic"
	"testing"

	"gochen/errors"
)

func TestRegistryRequiresValidationAndFreezesActualHandler(t *testing.T) {
	r, err := NewRegistry(NewTaskSupervisor("worker"))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	if err := r.Register("work", func(context.Context) error { calls.Add(1); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(t.Context(), "work"); !errors.Is(err, errors.FailedPrecondition) || calls.Load() != 0 {
		t.Fatalf("unvalidated run: %v, calls=%d", err, calls.Load())
	}
	if err := r.ActivateHandlers(); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("activation before seal: %v", err)
	}
	if err := r.Register("work", func(context.Context) error { return nil }); !errors.Is(err, errors.Conflict) {
		t.Fatalf("handler replacement: %v", err)
	}
	if err := r.SealHandlers(); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(t.Context(), "work"); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("sealed but unvalidated run: %v", err)
	}
	if err := r.Register("late", func(context.Context) error { return nil }); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("late handler: %v", err)
	}
	if err := r.ActivateHandlers(); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(t.Context(), "work"); err != nil || calls.Load() != 1 {
		t.Fatalf("validated run: %v, calls=%d", err, calls.Load())
	}
	names := r.RegisteredHandlers()
	names[0] = "mutated"
	if r.RegisteredHandlers()[0] != "work" {
		t.Fatal("registered names were mutable")
	}
	if err := r.Go(t.Context(), "work"); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(t.Context()); err != nil || calls.Load() != 2 {
		t.Fatalf("stop: %v, calls=%d", err, calls.Load())
	}
	if err := r.Run(t.Context(), "work"); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("stopped registry admitted a task: %v", err)
	}
	if err := r.ActivateHandlers(); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(t.Context(), "work"); err != nil || calls.Load() != 3 {
		t.Fatalf("restart changed handler: %v, calls=%d", err, calls.Load())
	}
	if err := r.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryPendingCleanupBlocksReactivation(t *testing.T) {
	r, err := NewRegistry(NewTaskSupervisor("worker"))
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	if err := r.Register("blocking", func(context.Context) error { close(started); <-release; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.SealHandlers(); err != nil {
		t.Fatal(err)
	}
	if err := r.ActivateHandlers(); err != nil {
		t.Fatal(err)
	}
	if err := r.Go(t.Context(), "blocking"); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.Stop(ctx); err == nil {
		t.Fatal("pending task unexpectedly stopped")
	}
	if err := r.ActivateHandlers(); !errors.Is(err, errors.Conflict) {
		t.Fatalf("pending cleanup allowed activation: %v", err)
	}
	close(release)
	if err := r.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := r.ActivateHandlers(); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}
