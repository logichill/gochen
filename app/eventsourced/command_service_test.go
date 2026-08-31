package eventsourced

import (
	"context"
	"strconv"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/app/operation"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing/store"
	"gochen/messaging"
	"gochen/messaging/command"
	"gochen/messaging/transport/memory"
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

	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Set", func() any { return &setEvent{} }))
	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*serviceAggregate, int64]{
		AggregateType:    "ServiceAggregate",
		EventIDGenerator: testEventIDGenerator(),
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

	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Set", func() any { return &setEvent{} }))
	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*serviceAggregate, int64]{
		AggregateType:    "ServiceAggregate",
		EventIDGenerator: testEventIDGenerator(),
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

	transport := memory.NewMemoryTransport(100, 1)
	require.NoError(t, transport.Start(ctx))
	bus := messaging.NewMessageBus(transport)
	handler := AsCommandMessageHandler[*serviceAggregate, int64](service, "Set", func(c *command.Command) (IEventSourcedCommand[int64], error) {
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

	err = bus.Publish(ctx, command.NewCommand("cmd-1", "Set", "1", "ServiceAggregate", 99))
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

func TestEventSourcedService_ExecuteCommandWithOperation(t *testing.T) {
	ctx := context.Background()

	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Set", func() any { return &setEvent{} }))
	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*serviceAggregate, int64]{
		AggregateType:    "ServiceAggregate",
		EventIDGenerator: testEventIDGenerator(),
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
	result, err := service.ExecuteCommandWithOperation(ctx, cmd, spec)
	require.NoError(t, err)
	require.Equal(t, operation.StatusSettled, result.Operation.Status)
	require.Equal(t, "7", result.Resource.ID)
	require.Equal(t, "service-aggregate", result.Resource.Type)
	require.Equal(t, []string{"aggregates:list"}, result.AffectedScopes)
}

func TestEventSourcedService_ExecuteCommandWithOperationRejectsTrackedWithoutRunner(t *testing.T) {
	ctx := context.Background()

	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Set", func() any { return &setEvent{} }))
	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*serviceAggregate, int64]{
		AggregateType:    "ServiceAggregate",
		EventIDGenerator: testEventIDGenerator(),
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

	result, err := service.ExecuteCommandWithOperation(ctx, &setCommand{ID: 7, V: 11}, &operation.Spec{
		Type: "aggregate.set",
		Mode: operation.ModeTracked,
	})

	require.Nil(t, result)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.FailedPrecondition))
}

func TestEventSourcedService_ExecuteCommandWithResolvedOperation(t *testing.T) {
	ctx := context.Background()

	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Set", func() any { return &setEvent{} }))
	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*serviceAggregate, int64]{
		AggregateType:    "ServiceAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*serviceAggregate, int64]("ServiceAggregate", &serviceAggregate{}, AdaptAggregateFactory(newServiceAggregate), adapter)
	require.NoError(t, err)

	runner := operation.NewRunner(&operation.RunnerOptions{
		IDGenerator: func() string { return "op_7" },
		StatusURLBuilder: func(operationID string) string {
			return "/operations/" + operationID
		},
		StreamURLBuilder: func(operationID string) string {
			return "/operations/" + operationID + "/stream"
		},
		DefaultRetryAfterMs: 800,
		Store:               operation.NewMemoryStore(),
	})
	service, err := NewEventSourcedService[*serviceAggregate, int64](repo, &EventSourcedServiceOptions[*serviceAggregate, int64]{
		OperationRunner: runner,
		OperationSpecResolver: func(ctx context.Context, cmd IEventSourcedCommand[int64]) (*operation.Spec, error) {
			return &operation.Spec{
				Type:           "aggregate.set",
				Mode:           operation.ModeTracked,
				Resource:       &operation.Resource{Type: "service-aggregate"},
				AffectedScopes: []string{"aggregates:list"},
			}, nil
		},
		OperationResultResolver: func(ctx context.Context, cmd IEventSourcedCommand[int64], spec *operation.Spec) (*operation.Result, error) {
			return operation.MergeResult(&operation.Result{
				Result: map[string]any{"value": cmd.(*setCommand).V},
			}, spec, &operation.Resource{ID: strconv.FormatInt(cmd.AggregateID(), 10)}), nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, service.RegisterCommandHandler(&setCommand{}, func(ctx context.Context, cmd IEventSourcedCommand[int64], agg *serviceAggregate) error {
		c := cmd.(*setCommand)
		return agg.ApplyAndRecord(&setEvent{V: c.V})
	}))

	result, err := service.ExecuteCommandWithResolvedOperation(ctx, &setCommand{ID: 7, V: 11})
	require.NoError(t, err)
	require.Equal(t, "op_7", result.Operation.ID)
	require.Equal(t, "aggregate.set", result.Operation.Type)
	require.Equal(t, operation.ModeTracked, result.Operation.Mode)
	require.Equal(t, operation.StatusAccepted, result.Operation.Status)
	require.Equal(t, "7", result.Resource.ID)
	require.Equal(t, "service-aggregate", result.Resource.Type)
	require.Equal(t, []string{"aggregates:list"}, result.AffectedScopes)
	require.Equal(t, "/operations/op_7", result.StatusURL)
	require.Equal(t, "/operations/op_7/stream", result.StreamURL)
	require.Equal(t, 800, result.RetryAfterMs)
	require.Equal(t, 11, result.Result["value"])
}

func TestEventSourcedService_ExecuteCommandWithResolvedOperationFallsBackWhenSpecNil(t *testing.T) {
	ctx := context.Background()

	eventStore := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("Set", func() any { return &setEvent{} }))
	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*serviceAggregate, int64]{
		AggregateType:    "ServiceAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*serviceAggregate, int64]("ServiceAggregate", &serviceAggregate{}, AdaptAggregateFactory(newServiceAggregate), adapter)
	require.NoError(t, err)

	service, err := NewEventSourcedService[*serviceAggregate, int64](repo, &EventSourcedServiceOptions[*serviceAggregate, int64]{
		OperationSpecResolver: func(ctx context.Context, cmd IEventSourcedCommand[int64]) (*operation.Spec, error) {
			return nil, nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, service.RegisterCommandHandler(&setCommand{}, func(ctx context.Context, cmd IEventSourcedCommand[int64], agg *serviceAggregate) error {
		c := cmd.(*setCommand)
		return agg.ApplyAndRecord(&setEvent{V: c.V})
	}))

	result, err := service.ExecuteCommandWithResolvedOperation(ctx, &setCommand{ID: 9, V: 22})
	require.NoError(t, err)
	require.Nil(t, result)

	loaded, err := repo.Get(ctx, 9)
	require.NoError(t, err)
	require.Equal(t, 22, loaded.Value)
}

func TestEventSourcedService_ExecuteCommandWithResolvedOperationRejectsNilCommandBeforeResolver(t *testing.T) {
	service, err := NewEventSourcedService[*retryTestAggregate, int64](&okRepo{}, &EventSourcedServiceOptions[*retryTestAggregate, int64]{
		OperationSpecResolver: func(ctx context.Context, cmd IEventSourcedCommand[int64]) (*operation.Spec, error) {
			t.Fatal("resolver should not be called for nil command")
			return nil, nil
		},
	})
	require.NoError(t, err)

	result, err := service.ExecuteCommandWithResolvedOperation(context.Background(), nil)
	require.Nil(t, result)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestEventSourcedService_RegisterCommandHandlerRejectsNilHandler(t *testing.T) {
	service, err := NewEventSourcedService[*retryTestAggregate, int64](&okRepo{}, nil)
	require.NoError(t, err)

	err = service.RegisterCommandHandler(&retryTestCommand{}, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestEventSourcedService_RegisterCommandHandlerRejectsDuplicate(t *testing.T) {
	service, err := NewEventSourcedService[*retryTestAggregate, int64](&okRepo{}, nil)
	require.NoError(t, err)

	handler := func(ctx context.Context, cmd IEventSourcedCommand[int64], aggregate *retryTestAggregate) error {
		return nil
	}
	require.NoError(t, service.RegisterCommandHandler(&retryTestCommand{}, handler))
	err = service.RegisterCommandHandler(&retryTestCommand{}, handler)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))
}
