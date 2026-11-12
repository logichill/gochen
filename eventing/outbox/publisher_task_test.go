package outbox

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"gochen/errors"
	"gochen/observe/logging"
)

type taskClaimSpy struct {
	MockOutboxRepository
	claims atomic.Int32
}

func (r *taskClaimSpy) ClaimPendingEntries(context.Context, int) ([]OutboxEntry[int64], error) {
	r.claims.Add(1)
	return nil, nil
}

func rawTaskPublisher(t *testing.T, parallel bool, repo IOutboxRepository[int64]) IOutboxPublisher {
	t.Helper()
	cfg := DefaultOutboxConfig()
	cfg.PublishInterval = time.Millisecond
	var publisher IOutboxPublisher
	var err error
	if parallel {
		publisher, err = NewParallelPublisher(repo, &MockEventBus{}, cfg, logging.NewNoopLogger(), 2, newTestRegistry(t), newTestUpgraders())
	} else {
		publisher, err = NewPublisher(repo, &MockEventBus{}, cfg, logging.NewNoopLogger(), newTestRegistry(t), newTestUpgraders())
	}
	if err != nil {
		t.Fatal(err)
	}
	return publisher
}

func TestOutboxTasksRequireBoundExecutorAndValidatedActualSource(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		t.Run(map[bool]string{false: "serial", true: "parallel"}[parallel], func(t *testing.T) {
			repo := &taskClaimSpy{}
			publisher := rawTaskPublisher(t, parallel, repo)
			expected := []string{"eventing.outbox.serial:cleanup_after_publish", "eventing.outbox.serial:poll_loop", "eventing.outbox.serial:publish_pending", "eventing.outbox.serial:renew_claim"}
			if parallel {
				expected = []string{"eventing.outbox.parallel:cleanup_loop", "eventing.outbox.parallel:context_watcher", "eventing.outbox.parallel:fetch_loop", "eventing.outbox.parallel:finalize_stop", "eventing.outbox.parallel:mark_loop", "eventing.outbox.parallel:publish_pending", "eventing.outbox.parallel:renew_claim", "eventing.outbox.parallel:worker_0", "eventing.outbox.parallel:worker_1"}
			}
			if actual := publisher.TaskRegistry().RegisteredHandlers(); !reflect.DeepEqual(actual, expected) {
				t.Fatalf("actual handler source=%v", actual)
			}
			if err := publisher.Start(t.Context()); !errors.Is(err, errors.FailedPrecondition) {
				t.Fatalf("unvalidated Start=%v", err)
			}
			if err := publisher.PublishPending(t.Context()); !errors.Is(err, errors.FailedPrecondition) {
				t.Fatalf("unvalidated publish=%v", err)
			}
			if err := publisher.SetTaskExecutor(nil); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("nil executor=%v", err)
			}
			if err := publisher.SetTaskExecutor(func(ctx context.Context, _ string, handler func(context.Context) error) error { return handler(ctx) }); err != nil {
				t.Fatal(err)
			}
			if err := publisher.TaskRegistry().SealHandlers(); err != nil {
				t.Fatal(err)
			}
			if err := publisher.Start(t.Context()); !errors.Is(err, errors.FailedPrecondition) {
				t.Fatalf("sealed-only Start=%v", err)
			}
			if err := publisher.SetTaskExecutor(func(context.Context, string, func(context.Context) error) error { return nil }); !errors.Is(err, errors.FailedPrecondition) {
				t.Fatalf("executor changed after source sealing: %v", err)
			}
			if repo.claims.Load() != 0 {
				t.Fatal("task executed before validation")
			}
			if err := publisher.TaskRegistry().ActivateHandlers(); err != nil {
				t.Fatal(err)
			}
			if err := publisher.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := publisher.Stop(stopCtx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOutboxTaskDenialPreventsResourceAccessAndPreservesStopCompletion(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		t.Run(map[bool]string{false: "serial", true: "parallel"}[parallel], func(t *testing.T) {
			repo := &taskClaimSpy{}
			publisher := rawTaskPublisher(t, parallel, repo)
			denied := errors.NewCode(errors.Forbidden, "ledger denied")
			if err := publisher.SetTaskExecutor(func(context.Context, string, func(context.Context) error) error { return denied }); err != nil {
				t.Fatal(err)
			}
			if err := publisher.TaskRegistry().SealHandlers(); err != nil {
				t.Fatal(err)
			}
			if err := publisher.TaskRegistry().ActivateHandlers(); err != nil {
				t.Fatal(err)
			}
			if err := publisher.PublishPending(t.Context()); !errors.Is(err, denied) {
				t.Fatalf("publish denial=%v", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			if err := publisher.Start(ctx); err != nil {
				t.Fatal(err)
			}
			cancel()
			stopCtx, stopCancel := context.WithTimeout(t.Context(), time.Second)
			defer stopCancel()
			if err := publisher.Stop(stopCtx); err != nil {
				t.Fatalf("denied tasks blocked lifecycle completion: %v", err)
			}
			if repo.claims.Load() != 0 {
				t.Fatalf("resource accessed after denial: %d", repo.claims.Load())
			}
		})
	}
}

func TestClaimTaskAuthorizationCompletesBeforePublicationAndReleasesOnFailure(t *testing.T) {
	for _, failure := range []string{"deny", "panic", "skip"} {
		t.Run(failure, func(t *testing.T) {
			repo := &MockOutboxRepository{}
			tasks, err := newPublisherTasks("test.claim", repo, logging.NewNoopLogger())
			if err != nil {
				t.Fatal(err)
			}
			if err := tasks.configure(func(context.Context, string, func(context.Context) error) error {
				switch failure {
				case "deny":
					return errors.NewCode(errors.Forbidden, "renew denied")
				case "panic":
					panic("executor panic")
				default:
					return nil
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := tasks.registry.SealHandlers(); err != nil {
				t.Fatal(err)
			}
			if err := tasks.registry.ActivateHandlers(); err != nil {
				t.Fatal(err)
			}
			calls := 0
			err = runWithClaimKeepalive(t.Context(), tasks, repo, newProcessingOutboxEntry(t, 1), time.Minute, time.Second, func(context.Context) error { calls++; return nil })
			if err == nil || calls != 0 {
				t.Fatalf("publication entered before claim authorization: calls=%d err=%v", calls, err)
			}
			stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := tasks.registry.Stop(stopCtx); err != nil {
				t.Fatalf("claim failure leaked task: %v", err)
			}
		})
	}
}

func TestOutboxExecutorCallbackIsSingleUseAndRevokedAfterReturn(t *testing.T) {
	tasks, err := newPublisherTasks("test.callback", &MockOutboxRepository{}, logging.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}
	var retained func(context.Context) error
	calls := 0
	if err := tasks.configure(func(ctx context.Context, _ string, handler func(context.Context) error) error {
		retained = handler
		if err := handler(ctx); err != nil {
			return err
		}
		return handler(ctx)
	}); err != nil {
		t.Fatal(err)
	}
	err = tasks.execute(t.Context(), "work", func(context.Context) error { calls++; return nil })
	if !errors.Is(err, errors.FailedPrecondition) || calls != 1 {
		t.Fatalf("repeated callback calls=%d err=%v", calls, err)
	}
	if err := retained(t.Context()); !errors.Is(err, errors.FailedPrecondition) || calls != 1 {
		t.Fatalf("retained callback calls=%d err=%v", calls, err)
	}
}
