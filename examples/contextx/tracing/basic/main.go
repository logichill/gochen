package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"gochen/app/eventsourced"
	"gochen/contextx"
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

// ValueSet 表示领域事件载荷。
type ValueSet struct{ V int }

// EventType 返回事件类型标识。
func (e *ValueSet) EventType() string { return "ValueSet" }

// Counter 表示示例用的计数器聚合。
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

// SetValue 表示设置值的命令。
type SetValue struct {
	ID int64
	V  int
}

// AggregateID 返回命令要操作的聚合 ID。
func (c *SetValue) AggregateID() int64 { return c.ID }

// main 演示 trace metadata 如何沿消息与事件链路传播。
func main() {
	log.SetPrefix("[tracing_demo] ")
	ctx := context.Background()
	metadataRegistry := deventsourced.NewMetadataRegistry()
	mustMetadata(metadataRegistry.Register(&Counter{}, "Counter"))

	reg := registry.NewRegistry()
	must(reg.Register("ValueSet", func() any { return &ValueSet{} }))
	upgraders := upcast.NewUpgraderRegistry()

	// 消息与事件总线：MessageBus 默认会贯通 metadata <-> message.Metadata（无需额外 middleware）
	messageBus := messaging.NewMessageBus(mtransport.NewMemoryTransport(1024, 2))
	eventBus := ebus.NewEventBus(messageBus)

	// 事件存储（内存） + ES 仓储
	storeAdapter, err := eventsourced.NewDomainEventStore(eventsourced.DomainEventStoreOptions[*Counter, int64]{
		AggregateType:    "Counter",
		EventStore:       estore.NewMemoryEventStore(),
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

	// 服务与命令处理（应用层）
	service, err := eventsourced.NewEventSourcedService[*Counter, int64](repo, nil)
	must(err)
	must(service.RegisterCommandHandler(&SetValue{}, func(ctx context.Context, cmd eventsourced.IEventSourcedCommand[int64], agg *Counter) error {
		c := cmd.(*SetValue)
		return agg.ApplyAndRecord(&ValueSet{V: c.V})
	}))

	// 订阅事件，打印元数据中的 trace 字段
	eventUnsub, err := eventBus.SubscribeEvent(ctx, "*", ebus.EventHandlerFunc(func(ctx context.Context, evt eventing.IEvent) error {
		md := evt.GetMetadata()
		traceID, _ := md.GetString(contextx.MetadataTraceKey)
		log.Printf("event=%s trace_id=%s", evt.GetType(), traceID)
		return nil
	}))
	must(err)
	defer func() { _ = eventUnsub(context.Background()) }()

	// 将服务适配为命令处理器
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

	// 分发命令；MessageBus 默认会把 trace 等信息贯通到 metadata，并在消费侧派生回 ctx
	aggID := int64(8080)
	must(messageBus.Publish(ctx, cmd.NewCommand("cmd-trace-1", "SetValue", strconv.FormatInt(aggID, 10), "Counter", 99)))
	time.Sleep(200 * time.Millisecond)

	loaded, err := repo.Get(ctx, aggID)
	must(err)
	fmt.Printf("Counter[%d] Value=%d\n", aggID, loaded.Value)
}

// must 在示例里用于快速失败，避免重复展开错误处理。
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func mustMetadata(_ *deventsourced.Metadata, err error) {
	must(err)
}
