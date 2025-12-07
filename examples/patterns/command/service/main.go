package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"gochen/app/eventsourced"
	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing"
	ebus "gochen/eventing/bus"
	estore "gochen/eventing/store"
	"gochen/messaging"
	cmd "gochen/messaging/command"
	mtransport "gochen/messaging/transport/memory"
)

// 领域事件载荷
type ValueSet struct{ V int }

func (e *ValueSet) EventType() string { return "ValueSet" }

// 聚合
type Counter struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

func NewCounter(id int64) *Counter {
	return &Counter{EventSourcedAggregate: deventsourced.NewEventSourcedAggregate[int64](id, "Counter")}
}

func (a *Counter) ApplyEvent(evt domain.IDomainEvent) error {
	switch p := evt.(type) {
	case *ValueSet:
		a.Value = p.V
	}
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

// 领域命令（实现 IEventSourcedCommand）
type SetValue struct {
	ID int64
	V  int
}

func (c *SetValue) AggregateID() int64 { return c.ID }

func main() {
	log.SetPrefix("[command_service] ")
	ctx := context.Background()

	// 消息与事件总线（内存传输）
	messageBus := messaging.NewMessageBus(mtransport.NewMemoryTransport(1024, 2))
	eventBus := ebus.NewEventBus(messageBus)

	// 事件存储（内存） + ES 仓储
	store := estore.NewMemoryEventStore()
	storeAdapter, err := eventsourced.NewDomainEventStore[*Counter](eventsourced.DomainEventStoreOptions[*Counter]{
		AggregateType: "Counter",
		Factory:       NewCounter,
		EventStore:    store,
		EventBus:      eventBus,
		PublishEvents: true,
	})
	must(err)

	repo, err := deventsourced.NewEventSourcedRepository[*Counter, int64]("Counter", NewCounter, storeAdapter)
	must(err)

	// 事件溯源服务（领域层定义）
	service, err := deventsourced.NewEventSourcedService[*Counter, int64](repo, nil)
	must(err)

	// 注册命令处理：SetValue -> 产生 ValueSet 事件
	must(service.RegisterCommandHandler(&SetValue{}, func(ctx context.Context, cmd deventsourced.IEventSourcedCommand[int64], agg *Counter) error {
		c := cmd.(*SetValue)
		return agg.ApplyAndRecord(&ValueSet{V: c.V})
	}))

	// 适配为命令消息处理器，并订阅到 MessageBus（按 command_type 匹配）
	handler := eventsourced.AsCommandMessageHandler[*Counter, int64](service, "SetValue", func(c *cmd.Command) (deventsourced.IEventSourcedCommand[int64], error) {
		v, _ := c.GetPayload().(int)
		return &SetValue{ID: c.GetAggregateID(), V: v}, nil
	})
	must(messageBus.Subscribe(ctx, messaging.MessageTypeCommand, handler))

	// 订阅打印事件
	_ = eventBus.SubscribeEvent(ctx, "*", ebus.EventHandlerFunc(func(ctx context.Context, evt eventing.IEvent) error {
		log.Printf("事件: %s v%d -> agg=%d payload=%#v", evt.GetType(), evt.GetVersion(), evt.GetAggregateID(), evt.GetPayload())
		return nil
	}))

	// 分发命令
	aggID := int64(5001)
	_ = messageBus.Publish(ctx, cmd.NewCommand("cmd-1", "SetValue", aggID, "Counter", 42))

	// 等待异步处理
	time.Sleep(200 * time.Millisecond)

	// 重放读取
	loaded, err := repo.GetByID(ctx, aggID)
	must(err)
	fmt.Printf("Counter[%d] Value=%d Version=%d\n", aggID, loaded.Value, loaded.GetVersion())
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
