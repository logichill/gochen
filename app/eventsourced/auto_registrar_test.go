package eventsourced

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/projection"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/messaging"
	directtransport "gochen/messaging/transport/direct"
)

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
	transport := directtransport.NewSyncTransport()
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
	err = registrar.RegisterProjections(ctx, &stringProjection{
		name:       "string-id-projection",
		eventTypes: []string{"StringIDEvent"},
	})

	require.NoError(t, err)
	_, err = manager.ProjectionStatus("string-id-projection")
	require.NoError(t, err)
}
