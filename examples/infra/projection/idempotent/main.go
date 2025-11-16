package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"gochen/eventing"
	ebus "gochen/eventing/bus"
	"gochen/eventing/projection"
	"gochen/examples/infra/projection/idempotent/internal/idemp"
	"gochen/examples/infra/projection/idempotent/internal/proj"
	"gochen/messaging"
	mtransport "gochen/messaging/transport/memory"
)

func main() {
	log.SetPrefix("[projection_idempotent_demo] ")
	ctx := context.Background()

	// 事件总线（内存）
	bus := ebus.NewEventBus(messaging.NewMessageBus(mtransport.NewMemoryTransport(1024, 2)))

	// 幂等存储 + 投影
	sink := idemp.NewSink()
	p, err := proj.NewIdempotentProjection(sink)
	must(err)

	pm := projection.NewProjectionManager(nil, bus)
	must(pm.RegisterProjection(p))
	must(pm.StartProjection(p.GetName()))

	// 发布重复事件（相同事件ID）
	evt1 := eventing.NewDomainEvent(10, "Cart", "ItemAdded", 1, &proj.ItemAdded{SKU: "ABC", Qty: 2})
	must(bus.PublishEvent(ctx, evt1))
	// 重复发送同一ID的事件（模拟去重场景）
	dup := *evt1
	must(bus.PublishEvent(ctx, &dup))

	// 发送新事件
	evt2 := eventing.NewDomainEvent(10, "Cart", "ItemAdded", 2, &proj.ItemAdded{SKU: "ABC", Qty: 3})
	must(bus.PublishEvent(ctx, evt2))

	time.Sleep(200 * time.Millisecond)
	fmt.Printf("SKU ABC stock=%d (expect 5)\n", proj.GetStock("ABC"))
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
