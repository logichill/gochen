package eventsourced

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing/store"
	"gochen/messaging"
	cmd "gochen/messaging/command"
	mtransport "gochen/messaging/transport/memory"
)

// 测试用领域事件与聚合
type setEvent struct{ V int }

func (e *setEvent) EventType() string { return "Set" }

type serviceAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

func newServiceAggregate(id int64) *serviceAggregate {
	return &serviceAggregate{
		EventSourcedAggregate: deventsourced.NewEventSourcedAggregate[int64](id, "ServiceAggregate"),
	}
}

func (a *serviceAggregate) ApplyEvent(evt domain.IDomainEvent) error {
	if e, ok := evt.(*setEvent); ok {
		a.Value = e.V
	}
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

type setCommand struct {
	ID int64
	V  int
}

func (c *setCommand) AggregateID() int64 { return c.ID }

func TestEventSourcedService_ExecuteCommand_Success(t *testing.T) {
	ctx := context.Background()

	eventStore := store.NewMemoryEventStore()
	adapter, err := NewDomainEventStore[*serviceAggregate](DomainEventStoreOptions[*serviceAggregate]{
		AggregateType: "ServiceAggregate",
		Factory:       newServiceAggregate,
		EventStore:    eventStore,
	})
	require.NoError(t, err)

	repo, err := deventsourced.NewEventSourcedRepository[*serviceAggregate]("ServiceAggregate", newServiceAggregate, adapter)
	require.NoError(t, err)

	service, err := deventsourced.NewEventSourcedService[*serviceAggregate](repo, nil)
	require.NoError(t, err)

	require.NoError(t, service.RegisterCommandHandler(&setCommand{}, func(ctx context.Context, cmd deventsourced.IEventSourcedCommand, agg *serviceAggregate) error {
		c := cmd.(*setCommand)
		return agg.ApplyAndRecord(&setEvent{V: c.V})
	}))

	err = service.ExecuteCommand(ctx, &setCommand{ID: 1, V: 42})
	require.NoError(t, err)

	loaded, err := repo.GetByID(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 42, loaded.Value)
}

func TestEventSourcedService_AsCommandMessageHandler(t *testing.T) {
	ctx := context.Background()

	eventStore := store.NewMemoryEventStore()
	adapter, err := NewDomainEventStore[*serviceAggregate](DomainEventStoreOptions[*serviceAggregate]{
		AggregateType: "ServiceAggregate",
		Factory:       newServiceAggregate,
		EventStore:    eventStore,
	})
	require.NoError(t, err)

	repo, err := deventsourced.NewEventSourcedRepository[*serviceAggregate]("ServiceAggregate", newServiceAggregate, adapter)
	require.NoError(t, err)

	service, err := deventsourced.NewEventSourcedService[*serviceAggregate](repo, nil)
	require.NoError(t, err)

	require.NoError(t, service.RegisterCommandHandler(&setCommand{}, func(ctx context.Context, cmd deventsourced.IEventSourcedCommand, agg *serviceAggregate) error {
		c := cmd.(*setCommand)
		return agg.ApplyAndRecord(&setEvent{V: c.V})
	}))

	transport := mtransport.NewMemoryTransport(100, 1)
	require.NoError(t, transport.Start(ctx))
	bus := messaging.NewMessageBus(transport)
	handler := AsCommandMessageHandler[*serviceAggregate](service, "Set", func(c *cmd.Command) (deventsourced.IEventSourcedCommand, error) {
		v, _ := c.GetPayload().(int)
		return &setCommand{ID: c.GetAggregateID(), V: v}, nil
	})
	require.NoError(t, bus.Subscribe(ctx, messaging.MessageTypeCommand, handler))

	err = bus.Publish(ctx, cmd.NewCommand("cmd-1", "Set", 1, "ServiceAggregate", 99))
	require.NoError(t, err)

	// 等待异步 worker 处理命令
	time.Sleep(50 * time.Millisecond)

	loaded, err := repo.GetByID(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 99, loaded.Value)
}
