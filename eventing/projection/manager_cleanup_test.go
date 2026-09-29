package projection

import (
	"context"
	stderrors "errors"
	"sync"
	"testing"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/internal/testutil"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/messaging"
	"gochen/messaging/transport/direct"
	"gochen/testkit/require"
)

func TestProjectionManager_DuplicateEventTypesSubscribeOnce(t *testing.T) {
	ctx := context.Background()
	transport := direct.NewSyncTransport()
	require.NoError(t, transport.Start(ctx))
	t.Cleanup(func() { require.NoError(t, transport.Stop(ctx)) })
	eventBus := bus.NewEventBus(messaging.NewMessageBus(transport))
	reg := registry.NewRegistry()
	require.NoError(t, reg.Register("Event", func() any { return &struct{}{} }))
	manager, err := NewProjectionManager[int64](nil, eventBus, reg, upcast.NewUpgraderRegistry())
	require.NoError(t, err)
	p := NewMockProjection("duplicate-events", []string{"Event", "Event"})
	release, err := manager.RegisterProjectionAny(ctx, p)
	require.NoError(t, err)
	require.NoError(t, manager.StartProjection(p.Name()))

	require.NoError(t, eventBus.PublishEvent(ctx, testutil.NewEvent[int64](1, "Agg", "Event", 1, nil)))
	if got := p.Status().ProcessedEvents; got != 1 {
		t.Errorf("processed events = %d, want 1", got)
	}
	if got := transport.Stats().HandlerCount; got != 1 {
		t.Errorf("subscriptions = %d, want 1", got)
	}
	require.NoError(t, release(ctx))
	if got := transport.Stats().HandlerCount; got != 0 {
		t.Errorf("subscriptions after release = %d, want 0", got)
	}
}

func TestProjectionManager_ReleaseDeadlineRetainsInFlightRuntime(t *testing.T) {
	for _, operation := range []string{"handle", "rebuild", "resume"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			eventBus := &MockEventBus{}
			reg := registry.NewRegistry()
			require.NoError(t, reg.Register("Event", func() any { return &struct{}{} }))
			eventStore := store.NewMemoryEventStore[int64]()
			manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upcast.NewUpgraderRegistry())
			require.NoError(t, err)
			if operation == "resume" {
				_, err := manager.WithCheckpointStore(NewMemoryCheckpointStore())
				require.NoError(t, err)
				require.NoError(t, eventStore.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{
					testutil.NewEvent[int64](1, "Agg", "Event", 1, nil),
				}, 0))
			}
			entered := make(chan struct{})
			blocked := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(blocked) })
			p := NewMockProjection("in-flight", []string{"Event"})
			block := func() error {
				close(entered)
				<-blocked
				return nil
			}
			p.handleFunc = func(context.Context, eventing.IEvent) error { return block() }
			p.rebuildFunc = func(context.Context, []eventing.Event[int64]) error { return block() }
			release, err := manager.RegisterProjectionAny(ctx, p)
			require.NoError(t, err)
			require.NoError(t, manager.StartProjection(p.Name()))
			rt, _ := manager.runtime(p.Name())
			handler := rt.handlers["Event"]
			operationDone := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				switch operation {
				case "handle":
					operationDone <- handler.HandleEvent(ctx, testutil.NewEvent[int64](1, "Agg", "Event", 1, nil))
				case "rebuild":
					operationDone <- manager.RebuildProjection(ctx, p.Name(), nil)
				case "resume":
					operationDone <- manager.ResumeFromCheckpoint(ctx, p.Name())
				}
			}()
			t.Cleanup(func() {
				unblock()
				<-finished
			})
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("operation did not start")
			}

			cleanupCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
			defer cancel()
			cleanupDone := make(chan error, 1)
			go func() { cleanupDone <- release(cleanupCtx) }()
			select {
			case err := <-cleanupDone:
				if !stderrors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("release error = %v, want DeadlineExceeded", err)
				}
			case <-time.After(time.Second):
				t.Fatal("release ignored its deadline")
			}
			status, err := manager.ProjectionStatus(p.Name())
			require.NoError(t, err)
			require.Equal(t, "cleanup_pending", status.Status)
			require.Contains(t, status.LastError, context.DeadlineExceeded.Error())
			require.Equal(t, 0, eventBus.unsubscribeCalls)
			err = manager.RegisterProjection(NewMockProjection(p.Name(), []string{"Event"}))
			require.True(t, errors.Is(err, errors.Conflict))

			unblock()
			require.NoError(t, <-operationDone)
			status, err = manager.ProjectionStatus(p.Name())
			require.NoError(t, err)
			require.Equal(t, "cleanup_pending", status.Status)
			require.Contains(t, status.LastError, context.DeadlineExceeded.Error())
			// 超时后未启动后台清理；由调用方使用新的 context 显式重试。
			require.NoError(t, release(ctx))
			require.Equal(t, 1, eventBus.unsubscribeCalls)
			_, err = manager.ProjectionStatus(p.Name())
			require.True(t, errors.Is(err, errors.NotFound))
			require.NoError(t, release(cleanupCtx))
		})
	}
}

func TestProjectionManager_ConcurrentReleaseHonorsDeadline(t *testing.T) {
	ctx := context.Background()
	eventBus := &duplicateUnsubscribeEventBus{
		MockEventBus: &MockEventBus{},
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	unblock := sync.OnceFunc(func() { close(eventBus.release) })
	manager, err := NewProjectionManager[int64](nil, eventBus, registry.NewRegistry(), upcast.NewUpgraderRegistry())
	require.NoError(t, err)
	release, err := manager.RegisterProjectionAny(ctx, NewMockProjection("concurrent-release", []string{"Event"}))
	require.NoError(t, err)
	firstDone := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		firstDone <- release(ctx)
	}()
	t.Cleanup(func() {
		unblock()
		<-finished
	})
	select {
	case <-eventBus.entered:
	case <-time.After(time.Second):
		t.Fatal("first release did not start")
	}

	cleanupCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	secondDone := make(chan error, 1)
	go func() { secondDone <- release(cleanupCtx) }()
	select {
	case err := <-secondDone:
		require.True(t, stderrors.Is(err, context.DeadlineExceeded))
	case <-time.After(time.Second):
		t.Fatal("concurrent release ignored its deadline")
	}
	require.Equal(t, 1, eventBus.unsubscribeCount())
	unblock()
	require.NoError(t, <-firstDone)
	require.NoError(t, release(ctx))
	require.Equal(t, 1, eventBus.unsubscribeCount())
}
