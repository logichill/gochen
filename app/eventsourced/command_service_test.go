package eventsourced

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/app/operation"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing/store"
	"gochen/messaging"
	cmd "gochen/messaging/command"
	mtransport "gochen/messaging/transport/memory"
)

// 测试用领域事件与聚合
type setEvent struct{ V int }

// EventType 返回事件类型标识。
//
// 返回：
// - result：文本结果
func (e *setEvent) EventType() string { return "Set" }

type serviceAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

// newServiceAggregate id：对象/实体标识。
//
// 参数：
//
// 返回：
// - result：返回的实例（类型：*serviceAggregate）
func newServiceAggregate(id int64) *serviceAggregate {
	a := &serviceAggregate{}
	agg, err := deventsourced.InitAggregate[int64](testMetadataRegistry, a, id, "ServiceAggregate")
	if err != nil {
		panic(err)
	}
	a.EventSourcedAggregate = agg
	return a
}

// ApplySetEvent 应用 setEvent。
func (a *serviceAggregate) ApplySetEvent(evt *setEvent) {
	a.Value = evt.V
}

type setCommand struct {
	ID int64
	V  int
}

// AggregateID 返回聚合标识。
//
// 返回：
// - result：数量/计数
func (c *setCommand) AggregateID() int64 { return c.ID }

// TestEventSourcedService_ExecuteCommand_Success 验证 EventSourcedService ExecuteCommand Success。
func TestEventSourcedService_ExecuteCommand_Success(t *testing.T) {
	ctx := context.Background()

	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Set", func() any { return &setEvent{} }))
	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*serviceAggregate, int64]{
		AggregateType:    "ServiceAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*serviceAggregate, int64]("ServiceAggregate", &serviceAggregate{}, AdaptAggregateFactory(newServiceAggregate), adapter)
	require.NoError(t, err)

	service, err := NewEventSourcedService[*serviceAggregate, int64](repo, nil)
	require.NoError(t, err)

	require.NoError(t, service.RegisterCommandHandler(&setCommand{}, func(ctx context.Context, cmd IEventSourcedCommand[int64], agg *serviceAggregate) error {
		c := cmd.(*setCommand)
		return agg.ApplyAndRecord(&setEvent{V: c.V})
	}))

	err = service.ExecuteCommand(ctx, &setCommand{ID: 1, V: 42})
	require.NoError(t, err)

	loaded, err := repo.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 42, loaded.Value)
}

// TestEventSourcedService_AsCommandMessageHandler 验证 EventSourcedService AsCommandMessageHandler。
func TestEventSourcedService_AsCommandMessageHandler(t *testing.T) {
	ctx := context.Background()

	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Set", func() any { return &setEvent{} }))
	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*serviceAggregate, int64]{
		AggregateType:    "ServiceAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*serviceAggregate, int64]("ServiceAggregate", &serviceAggregate{}, AdaptAggregateFactory(newServiceAggregate), adapter)
	require.NoError(t, err)

	service, err := NewEventSourcedService[*serviceAggregate, int64](repo, nil)
	require.NoError(t, err)

	require.NoError(t, service.RegisterCommandHandler(&setCommand{}, func(ctx context.Context, cmd IEventSourcedCommand[int64], agg *serviceAggregate) error {
		c := cmd.(*setCommand)
		return agg.ApplyAndRecord(&setEvent{V: c.V})
	}))

	transport := mtransport.NewMemoryTransport(100, 1)
	require.NoError(t, transport.Start(ctx))
	bus := messaging.NewMessageBus(transport)
	handler := AsCommandMessageHandler[*serviceAggregate, int64](service, "Set", func(c *cmd.Command) (IEventSourcedCommand[int64], error) {
		v, _ := messaging.PayloadAs[int](c.GetPayload())
		aggID, err := strconv.ParseInt(c.GetAggregateID(), 10, 64)
		if err != nil {
			return nil, err
		}
		return &setCommand{ID: aggID, V: v}, nil
	})
	unsub, err := bus.Subscribe(ctx, "Set", handler)
	require.NoError(t, err)
	t.Cleanup(func() { _ = unsub(context.Background()) })

	err = bus.Publish(ctx, cmd.NewCommand("cmd-1", "Set", "1", "ServiceAggregate", 99))
	require.NoError(t, err)

	// 等待异步 worker 处理命令（避免固定 sleep 导致不稳定）
	require.Eventually(t, func() bool {
		loaded, lerr := repo.Get(ctx, 1)
		if lerr != nil {
			return false
		}
		return loaded.Value == 99
	}, 500*time.Millisecond, 10*time.Millisecond)
}

func TestEventSourcedService_ExecuteCommandWrappedByOperationRunner(t *testing.T) {
	ctx := context.Background()

	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Set", func() any { return &setEvent{} }))
	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*serviceAggregate, int64]{
		AggregateType:    "ServiceAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*serviceAggregate, int64]("ServiceAggregate", &serviceAggregate{}, AdaptAggregateFactory(newServiceAggregate), adapter)
	require.NoError(t, err)

	service, err := NewEventSourcedService[*serviceAggregate, int64](repo, nil)
	require.NoError(t, err)
	require.NoError(t, service.RegisterCommandHandler(&setCommand{}, func(ctx context.Context, cmd IEventSourcedCommand[int64], agg *serviceAggregate) error {
		c := cmd.(*setCommand)
		return agg.ApplyAndRecord(&setEvent{V: c.V})
	}))

	spec := &operation.Spec{
		Type:           "aggregate.set",
		Mode:           operation.ModeInline,
		Resource:       &operation.Resource{Type: "service-aggregate"},
		AffectedScopes: []string{"aggregates:list"},
	}
	cmd := &setCommand{ID: 7, V: 11}
	result, err := operation.DefaultRunner().Execute(ctx, spec, func(ctx context.Context) (*operation.Result, error) {
		if err := service.ExecuteCommand(ctx, cmd); err != nil {
			return nil, err
		}
		return operation.MergeResult(nil, spec, &operation.Resource{ID: strconv.FormatInt(cmd.AggregateID(), 10)}), nil
	})
	require.NoError(t, err)
	require.Equal(t, operation.StatusSettled, result.Operation.Status)
	require.Equal(t, "7", result.Resource.ID)
	require.Equal(t, "service-aggregate", result.Resource.Type)
	require.Equal(t, []string{"aggregates:list"}, result.AffectedScopes)
}
