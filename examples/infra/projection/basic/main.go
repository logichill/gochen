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
	"gochen/examples/infra/projection/basic/internal/proj"
	"gochen/messaging"
	mtransport "gochen/messaging/transport/memory"
)

// main 演示基础投影、检查点与恢复追赶的最小流程。
func main() {
	log.SetPrefix("[projection_demo] ")
	ctx := context.Background()

	// 事件总线（内存）
	bus := ebus.NewEventBus(messaging.NewMessageBus(mtransport.NewMemoryTransport(1024, 2)))

	// 事件存储 + 注册表 + 升级器（用于 checkpoint 重放时的 payload upgrade/hydration）
	eventStore := store.NewMemoryEventStore()
	reg := registry.NewRegistry()
	must(reg.RegisterWithVersion("OrderCreated", 1, func() any { return &proj.OrderCreated{} }))
	upgraders := upcast.NewUpgraderRegistry()

	// 投影管理器 + 检查点（内存）
	pm, err := projection.NewProjectionManager[int64](eventStore, bus, reg, upgraders)
	must(err)
	pm, err = pm.WithCheckpointStore(projection.NewMemoryCheckpointStore())
	must(err)

	// 注册投影
	p, err := proj.NewOrderAmountProjection()
	must(err)
	must(pm.RegisterProjection(p))
	must(pm.StartProjection(p.Name()))

	// 发布事件
	var expectedVersion uint64
	for i := 1; i <= 3; i++ {
		evt := eventing.NewEvent[int64](int64(1000), "Order", "OrderCreated", uint64(i), &proj.OrderCreated{Amount: i * 10})
		must(eventStore.AppendEvents(ctx, 1000, []eventing.IStorableEvent[int64]{evt}, expectedVersion))
		expectedVersion++
		must(bus.PublishEvent(ctx, evt))
	}
	time.Sleep(200 * time.Millisecond)

	// 查看投影状态与读模型
	st, err := pm.ProjectionStatus(p.Name())
	must(err)
	fmt.Printf("Projection=%s Processed=%d TotalAmount=%d\n", st.Name, st.ProcessedEvents, proj.GetTotal())

	// 模拟“重启后追赶”
	must(pm.StopProjection(p.Name()))
	must(pm.ResumeFromCheckpoint(ctx, p.Name()))
	for i := 4; i <= 5; i++ {
		evt := eventing.NewEvent[int64](int64(1000), "Order", "OrderCreated", uint64(i), &proj.OrderCreated{Amount: i * 10})
		must(eventStore.AppendEvents(ctx, 1000, []eventing.IStorableEvent[int64]{evt}, expectedVersion))
		expectedVersion++
		must(bus.PublishEvent(ctx, evt))
	}
	time.Sleep(200 * time.Millisecond)
	st, err = pm.ProjectionStatus(p.Name())
	must(err)
	fmt.Printf("After resume -> Processed=%d TotalAmount=%d\n", st.ProcessedEvents, proj.GetTotal())
}

// must 在示例里用于快速失败，避免重复编写样板错误处理。
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
