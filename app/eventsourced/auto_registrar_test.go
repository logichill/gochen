package eventsourced

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/projection"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/messaging"
	"gochen/messaging/transport/direct"
)

func TestEventSourcedAutoRegistrar_UnregisterProjectionsAttemptsAllAndJoinsErrors(t *testing.T) {
	registrar := NewEventSourcedAutoRegistrar(nil, nil)
	firstErr := stderrors.New("first release failed")
	lastErr := stderrors.New("last release failed")
	var calls []int
	retry := false
	unsubs := []messaging.UnsubscribeFunc{
		func(context.Context) error {
			calls = append(calls, 0)
			if !retry {
				return firstErr
			}
			return nil
		},
		nil,
		func(context.Context) error {
			calls = append(calls, 2)
			return nil
		},
		func(context.Context) error {
			calls = append(calls, 3)
			if !retry {
				return lastErr
			}
			return nil
		},
	}
	err := registrar.UnregisterProjections(context.Background(), unsubs...)
	require.Equal(t, []int{3, 2, 0}, calls)
	require.True(t, stderrors.Is(err, firstErr))
	require.True(t, stderrors.Is(err, lastErr))

	retry = true
	calls = nil
	require.NoError(t, registrar.UnregisterProjections(context.Background(), unsubs...))
	require.Equal(t, []int{3, 2, 0}, calls)
}

type stringProjection struct {
	name       string
	eventTypes []string
}

func (p *stringProjection) Name() string {
	return p.name
}

func (p *stringProjection) Handle(ctx context.Context, event eventing.IEvent) error {
	return nil
}

func (p *stringProjection) SupportedEventTypes() []string {
	return p.eventTypes
}

func (p *stringProjection) Rebuild(ctx context.Context, events []eventing.Event[string]) error {
	return nil
}

func (p *stringProjection) Status() projection.ProjectionStatus {
	return projection.ProjectionStatus{
		Name:      p.name,
		Status:    "stopped",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func TestEventSourcedAutoRegistrar_RegisterProjectionsSupportsNonInt64Manager(t *testing.T) {
	ctx := context.Background()
	transport := direct.NewSyncTransport()
	require.NoError(t, transport.Start(ctx))
	defer func() { _ = transport.Stop(context.Background()) }()

	eventBus := bus.NewEventBus(messaging.NewMessageBus(transport))
	manager, err := projection.NewProjectionManager[string](
		nil,
		eventBus,
		registry.NewRegistry(),
		upcast.NewUpgraderRegistry(),
	)
	require.NoError(t, err)

	registrar := NewEventSourcedAutoRegistrar(eventBus, manager)
	unsubs, err := registrar.RegisterProjections(ctx, &stringProjection{
		name:       "string-id-projection",
		eventTypes: []string{"StringIDEvent"},
	})

	require.NoError(t, err)
	_, err = manager.ProjectionStatus("string-id-projection")
	require.NoError(t, err)
	require.NoError(t, registrar.UnregisterProjections(ctx, unsubs...))
}

func TestEventSourcedAutoRegistrar_FailedBatchReturnsAllCleanupFunctions(t *testing.T) {
	ctx := context.Background()
	eventBus := &partialProjectionBus{}
	manager, err := projection.NewProjectionManager[string](nil, eventBus, registry.NewRegistry(), upcast.NewUpgraderRegistry())
	require.NoError(t, err)
	registrar := NewEventSourcedAutoRegistrar(eventBus, manager)
	unsubs, err := registrar.RegisterProjections(ctx,
		&stringProjection{name: "first", eventTypes: []string{"ok"}},
		&stringProjection{name: "partial", eventTypes: []string{"retry", "fail"}},
	)
	require.Error(t, err)
	require.Equal(t, 2, len(unsubs))
	status, err := manager.ProjectionStatus("partial")
	require.NoError(t, err)
	require.Equal(t, "cleanup_pending", status.Status)
	require.NoError(t, registrar.UnregisterProjections(ctx, unsubs...))
	require.NoError(t, registrar.UnregisterProjections(ctx, unsubs...))
	for _, name := range []string{"first", "partial"} {
		_, err := manager.ProjectionStatus(name)
		require.True(t, errors.Is(err, errors.NotFound))
	}
	require.Equal(t, 2, eventBus.retryAttempts)
}

type partialProjectionBus struct {
	bus.IEventBus
	retryAttempts int
}

func (b *partialProjectionBus) SubscribeEvent(_ context.Context, eventType string, _ bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	if eventType == "fail" {
		return nil, errors.NewCode(errors.Dependency, "subscription failed")
	}
	return func(context.Context) error {
		if eventType == "retry" {
			b.retryAttempts++
			if b.retryAttempts == 1 {
				return errors.NewCode(errors.Dependency, "unsubscribe failed")
			}
		}
		return nil
	}, nil
}
