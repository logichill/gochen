package eventsourced

import (
	"context"
	stderrors "errors"
	"testing"

	"gochen/errors"
	"gochen/eventing/bus"
	"gochen/eventing/projection"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/messaging"
	"gochen/messaging/transport/direct"
	"gochen/testkit/require"
)

// retryCleanupTransport 在真实内存订阅上注入失败，验证完整总线调用链。
type retryCleanupTransport struct {
	*direct.SyncTransport
	failures     map[string]int
	cleanupErrs  map[string]error
	calls        []string
	subscribeErr error
}

func (t *retryCleanupTransport) Subscribe(ctx context.Context, kind string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	if kind == "fail" {
		return nil, t.subscribeErr
	}
	unsub, err := t.SyncTransport.Subscribe(ctx, kind, handler)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		t.calls = append(t.calls, kind)
		if t.failures[kind] > 0 {
			t.failures[kind]--
			return t.cleanupErrs[kind]
		}
		return unsub(ctx)
	}, nil
}

type cleanupHandler struct {
	bus.EventHandlerFunc
	kinds []string
}

func (h cleanupHandler) EventTypes() []string { return h.kinds }

func TestEventSourcedAutoRegistrar_ProjectionReleaseRetriesThroughMessageBus(t *testing.T) {
	ctx := context.Background()
	cleanupErr := stderrors.New("unsubscribe failed")
	transport := &retryCleanupTransport{
		SyncTransport: direct.NewSyncTransport(),
		failures:      map[string]int{"Event": 1},
		cleanupErrs:   map[string]error{"Event": cleanupErr},
	}
	eventBus := bus.NewEventBus(messaging.NewMessageBus(transport))
	manager, err := projection.NewProjectionManager[string](nil, eventBus, registry.NewRegistry(), upcast.NewUpgraderRegistry())
	require.NoError(t, err)
	registrar := NewEventSourcedAutoRegistrar(eventBus, manager)
	unsubs, err := registrar.RegisterProjections(ctx, &stringProjection{name: "retry", eventTypes: []string{"Event"}})
	require.NoError(t, err)
	err = registrar.UnregisterProjections(ctx, unsubs...)
	require.True(t, stderrors.Is(err, cleanupErr))
	status, err := manager.ProjectionStatus("retry")
	require.NoError(t, err)
	require.Equal(t, "cleanup_pending", status.Status)
	require.Equal(t, 1, transport.Stats().HandlerCount)
	require.NoError(t, registrar.UnregisterProjections(ctx, unsubs...))
	require.NoError(t, registrar.UnregisterProjections(ctx, unsubs...))
	require.Equal(t, []string{"Event", "Event"}, transport.calls)
	require.Equal(t, 0, transport.Stats().HandlerCount)
	_, err = manager.ProjectionStatus("retry")
	require.True(t, errors.Is(err, errors.NotFound))
}

func TestEventSourcedAutoRegistrar_HandlerRollbackRetainsOnlyFailures(t *testing.T) {
	ctx := context.Background()
	subscribeErr := stderrors.New("subscribe failed")
	aErr, cErr := stderrors.New("A cleanup failed"), stderrors.New("C cleanup failed")
	transport := &retryCleanupTransport{
		SyncTransport: direct.NewSyncTransport(),
		failures:      map[string]int{"A": 1, "C": 1},
		cleanupErrs:   map[string]error{"A": aErr, "C": cErr},
		subscribeErr:  subscribeErr,
	}
	registrar := NewEventSourcedAutoRegistrar(bus.NewEventBus(messaging.NewMessageBus(transport)), nil)
	unsubs, err := registrar.RegisterHandlers(ctx,
		cleanupHandler{kinds: []string{"A"}},
		cleanupHandler{kinds: []string{"B"}},
		cleanupHandler{kinds: []string{"C"}},
		cleanupHandler{kinds: []string{"fail"}},
	)
	require.True(t, stderrors.Is(err, subscribeErr))
	require.True(t, stderrors.Is(err, aErr))
	require.True(t, stderrors.Is(err, cErr))
	require.Equal(t, 2, len(unsubs))
	require.Equal(t, 2, transport.Stats().HandlerCount)
	require.Equal(t, []string{"C", "B", "A"}, transport.calls)
	require.NoError(t, registrar.UnregisterHandlers(ctx, unsubs...))
	require.NoError(t, registrar.UnregisterHandlers(ctx, unsubs...))
	require.Equal(t, []string{"C", "B", "A", "C", "A"}, transport.calls)
	require.Equal(t, 0, transport.Stats().HandlerCount)
}

func TestEventSourcedAutoRegistrar_PartialHandlerRegistrationRetainsCleanup(t *testing.T) {
	ctx := context.Background()
	subscribeErr, cleanupErr := stderrors.New("subscribe failed"), stderrors.New("cleanup failed")
	for _, failures := range []int{0, 2} {
		t.Run(map[int]string{0: "rollback succeeds", 2: "nested rollback needs retry"}[failures], func(t *testing.T) {
			transport := &retryCleanupTransport{
				SyncTransport: direct.NewSyncTransport(),
				failures:      map[string]int{"partial": failures},
				cleanupErrs:   map[string]error{"partial": cleanupErr},
				subscribeErr:  subscribeErr,
			}
			registrar := NewEventSourcedAutoRegistrar(bus.NewEventBus(messaging.NewMessageBus(transport)), nil)
			unsubs, err := registrar.RegisterHandlers(ctx,
				cleanupHandler{kinds: []string{"earlier"}},
				cleanupHandler{kinds: []string{"partial", "fail"}},
			)
			require.True(t, stderrors.Is(err, subscribeErr))
			if failures > 0 {
				require.True(t, stderrors.Is(err, cleanupErr))
				require.Equal(t, 1, len(unsubs))
				require.Equal(t, 1, transport.Stats().HandlerCount)
				require.Equal(t, []string{"partial", "partial", "earlier"}, transport.calls)
				require.NoError(t, registrar.UnregisterHandlers(ctx, unsubs...))
				require.NoError(t, registrar.UnregisterHandlers(ctx, unsubs...))
				require.Equal(t, []string{"partial", "partial", "earlier", "partial"}, transport.calls)
			} else {
				require.Equal(t, 0, len(unsubs))
				require.Equal(t, []string{"partial", "earlier"}, transport.calls)
			}
			require.Equal(t, 0, transport.Stats().HandlerCount)
		})
	}
}
