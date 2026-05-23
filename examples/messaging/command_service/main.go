package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"gochen/app/eventsourced"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing"
	ebus "gochen/eventing/bus"
	"gochen/eventing/registry"
	estore "gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/messaging"
	cmd "gochen/messaging/command"
	mtransport "gochen/messaging/transport/memory"
)

// 领域事件载荷
type ValueSet struct{ V int }

// EventType 返回事件类型标识。
func (e *ValueSet) EventType() string { return "ValueSet" }

// Counter 演示通过命令总线驱动的事件溯源计数器。
type Counter struct {
	*deventsourced.EventSourcedAggregate[int64] `aggregate:"Counter"`
	Value                                       int
}

// NewCounter 创建一个新的计数器聚合。
func NewCounter(metadataRegistry *deventsourced.MetadataRegistry, id int64) *Counter {
	counter, err := deventsourced.New[Counter, int64](metadataRegistry, id)
	if err != nil {
		panic(err)
	}
	return counter
}

// ApplyValueSet 用事件中的值覆盖当前计数器状态。
func (a *Counter) ApplyValueSet(evt *ValueSet) {
	a.Value = evt.V
}

// 领域命令（实现 IEventSourcedCommand）
type SetValue struct {
	ID int64
	V  int
}

// AggregateID 返回命令要操作的聚合 ID。
func (c *SetValue) AggregateID() int64 { return c.ID }

// main 演示命令消息、事件溯源服务和消息总线的串联方式。
func main() {
	log.SetPrefix("[command_service] ")
	ctx := context.Background()
	metadataRegistry := deventsourced.NewMetadataRegistry()
	mustMetadata(metadataRegistry.Register(&Counter{}, "Counter"))

	// 消息与事件总线（内存传输）
	messageBus := messaging.NewMessageBus(mtransport.NewMemoryTransport(1024, 2))
	eventBus := ebus.NewEventBus(messageBus)
	reg := registry.NewRegistry()
	must(reg.Register("ValueSet", func() any { return &ValueSet{} }))
	upgraders := upcast.NewUpgraderRegistry()

	// 事件存储（内存） + ES 仓储
	store := estore.NewMemoryEventStore()
	storeAdapter, err := eventsourced.NewDomainEventStore(eventsourced.DomainEventStoreOptions[*Counter, int64]{
		AggregateType:    "Counter",
		EventStore:       store,
		EventBus:         eventBus,
		PublishEvents:    true,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	must(err)

	repo, err := eventsourced.NewEventSourcedRepository(eventsourced.RepositoryOptions[*Counter, int64]{
		AggregateType:    "Counter",
		Sample:           &Counter{},
		Factory:          func(id int64) (*Counter, error) { return NewCounter(metadataRegistry, id), nil },
		Store:            storeAdapter,
		MetadataRegistry: metadataRegistry,
	})
	must(err)

	// 事件溯源命令执行服务（应用层）
	service, err := eventsourced.NewEventSourcedService[*Counter, int64](repo, nil)
	must(err)

	// 注册命令处理：SetValue -> 产生 ValueSet 事件
	must(service.RegisterCommandHandler(&SetValue{}, func(ctx context.Context, cmd eventsourced.IEventSourcedCommand[int64], agg *Counter) error {
		c := cmd.(*SetValue)
		return agg.ApplyAndRecord(&ValueSet{V: c.V})
	}))

	// 适配为命令消息处理器，并订阅到 MessageBus（按命令名/Type 匹配）
	handler := eventsourced.AsCommandMessageHandler[*Counter, int64](service, "SetValue", func(c *cmd.Command) (eventsourced.IEventSourcedCommand[int64], error) {
		v, _ := messaging.PayloadAs[int](c.GetPayload())
		aggID, err := strconv.ParseInt(c.GetAggregateID(), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid aggregate_id: %w", err)
		}
		return &SetValue{ID: aggID, V: v}, nil
	})
	unsub, err := messageBus.Subscribe(ctx, "SetValue", handler)
	must(err)
	defer func() { _ = unsub(context.Background()) }()

	// 订阅打印事件
	eventUnsub, err := eventBus.SubscribeEvent(ctx, "*", ebus.EventHandlerFunc(func(ctx context.Context, evt eventing.IEvent) error {
		aggID := int64(0)
		if typed, ok := evt.(eventing.ITypedEvent[int64]); ok {
			aggID = typed.GetAggregateID()
		}
		log.Printf("event: %s v%d -> agg=%d payload=%#v", evt.GetType(), evt.GetVersion(), aggID, messaging.PayloadValue(evt.GetPayload()))
		return nil
	}))
	must(err)
	defer func() { _ = eventUnsub(context.Background()) }()

	// 分发命令
	aggID := int64(5001)
	must(messageBus.Publish(ctx, cmd.NewCommand("cmd-1", "SetValue", strconv.FormatInt(aggID, 10), "Counter", 42)))

	// 等待异步处理
	time.Sleep(200 * time.Millisecond)

	// 重放读取
	loaded, err := repo.Get(ctx, aggID)
	must(err)
	fmt.Printf("Counter[%d] Value=%d Version=%d\n", aggID, loaded.Value, loaded.GetVersion())
}

// must 在示例里用于快速失败，避免铺开样板错误处理。
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func mustMetadata(_ *deventsourced.Metadata, err error) {
	must(err)
}
