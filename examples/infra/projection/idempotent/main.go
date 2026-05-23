package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"gochen/eventing"
	ebus "gochen/eventing/bus"
	"gochen/eventing/projection"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/examples/infra/projection/idempotent/internal/idemp"
	"gochen/examples/infra/projection/idempotent/internal/proj"
	"gochen/messaging"
	mtransport "gochen/messaging/transport/memory"
)

// main 演示幂等投影如何跳过重复事件。
func main() {
	log.SetPrefix("[projection_idempotent_demo] ")
	ctx := context.Background()

	// 事件总线（内存）
	bus := ebus.NewEventBus(messaging.NewMessageBus(mtransport.NewMemoryTransport(1024, 2)))

	// 幂等存储 + 投影
	sink := idemp.NewSink()
	p, err := proj.NewIdempotentProjection(sink)
	must(err)

	eventStore := store.NewMemoryEventStore()
	reg := registry.NewRegistry()
	must(reg.RegisterWithVersion("ItemAdded", 1, func() any { return &proj.ItemAdded{} }))
	upgraders := upcast.NewUpgraderRegistry()

	pm, err := projection.NewProjectionManager[int64](eventStore, bus, reg, upgraders)
	must(err)
	must(pm.RegisterProjection(p))
	must(pm.StartProjection(p.Name()))

	// 发布重复事件（相同事件ID）
	evt1 := eventing.NewEvent(10, "Cart", "ItemAdded", 1, &proj.ItemAdded{SKU: "ABC", Qty: 2})
	must(bus.PublishEvent(ctx, evt1))
	// 重复发送同一ID的事件（模拟去重场景）
	dup := *evt1
	must(bus.PublishEvent(ctx, &dup))

	// 发送新事件
	evt2 := eventing.NewEvent(10, "Cart", "ItemAdded", 2, &proj.ItemAdded{SKU: "ABC", Qty: 3})
	must(bus.PublishEvent(ctx, evt2))

	time.Sleep(200 * time.Millisecond)
	fmt.Printf("SKU ABC stock=%d (expect 5)\n", proj.GetStock("ABC"))
}

// must 在示例里用于快速失败，避免重复展开错误处理。
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
