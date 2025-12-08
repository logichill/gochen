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
	mmw "gochen/messaging/middleware"
	mtransport "gochen/messaging/transport/memory"
)

type ValueSet struct{ V int }

func (e *ValueSet) EventType() string { return "ValueSet" }

type Counter struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

func NewCounter(id int64) *Counter {
	return &Counter{EventSourcedAggregate: deventsourced.NewEventSourcedAggregate[int64](id, "Counter")}
}

func (a *Counter) ApplyEvent(evt domain.IDomainEvent) error {
	if p, ok := evt.(*ValueSet); ok {
		a.Value = p.V
	}
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

type SetValue struct {
	ID int64
	V  int
}

func (c *SetValue) AggregateID() int64 { return c.ID }

func main() {
	log.SetPrefix("[tracing_demo] ")
	ctx := context.Background()

	// 消息与事件总线：安装 TracingMiddleware
	messageBus := messaging.NewMessageBus(mtransport.NewMemoryTransport(1024, 2))
	messageBus.Use(mmw.NewTracingMiddleware())
	eventBus := ebus.NewEventBus(messageBus)

	// 事件存储（内存） + ES 仓储
	storeAdapter, err := eventsourced.NewDomainEventStore(eventsourced.DomainEventStoreOptions[*Counter, int64]{
		AggregateType: "Counter",
		Factory:       NewCounter,
		EventStore:    estore.NewMemoryEventStore(),
		EventBus:      eventBus,
		PublishEvents: true,
	})
	must(err)

	repo, err := deventsourced.NewEventSourcedRepository[*Counter, int64]("Counter", NewCounter, storeAdapter)
	must(err)

	// 服务与命令处理（领域层定义）
	service, err := deventsourced.NewEventSourcedService[*Counter, int64](repo, nil)
	must(err)
	must(service.RegisterCommandHandler(&SetValue{}, func(ctx context.Context, cmd deventsourced.IEventSourcedCommand[int64], agg *Counter) error {
		c := cmd.(*SetValue)
		return agg.ApplyAndRecord(&ValueSet{V: c.V})
	}))

	// 订阅事件，打印元数据中的 trace 字段
	must(eventBus.SubscribeEvent(ctx, "*", ebus.EventHandlerFunc(func(ctx context.Context, evt eventing.IEvent) error {
		md := evt.GetMetadata()
		log.Printf("event=%s corr=%v caus=%v trace=%v", evt.GetType(), md[mmw.KeyCorrelationID], md[mmw.KeyCausationID], md[mmw.KeyTraceID])
		return nil
	})))

	// 将服务适配为命令处理器
	handler := eventsourced.AsCommandMessageHandler[*Counter, int64](service, "SetValue", func(c *cmd.Command) (deventsourced.IEventSourcedCommand[int64], error) {
		v, _ := c.GetPayload().(int)
		return &SetValue{ID: c.GetAggregateID(), V: v}, nil
	})
	must(messageBus.Subscribe(ctx, messaging.MessageTypeCommand, handler))

	// 分发命令；TracingMiddleware 会为命令注入 corr/caus/trace，并通过 Context 传给后续事件
	aggID := int64(8080)
	must(messageBus.Publish(ctx, cmd.NewCommand("cmd-trace-1", "SetValue", aggID, "Counter", 99)))
	time.Sleep(200 * time.Millisecond)

	loaded, err := repo.GetByID(ctx, aggID)
	must(err)
	fmt.Printf("Counter[%d] Value=%d\n", aggID, loaded.Value)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
