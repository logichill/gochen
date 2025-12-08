package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"gochen/eventing"
	ebus "gochen/eventing/bus"
	"gochen/eventing/projection"
	"gochen/examples/infra/projection/basic/internal/proj"
	"gochen/messaging"
	mtransport "gochen/messaging/transport/memory"
)

func main() {
	log.SetPrefix("[projection_demo] ")
	ctx := context.Background()

	// 事件总线（内存）
	bus := ebus.NewEventBus(messaging.NewMessageBus(mtransport.NewMemoryTransport(1024, 2)))

	// 投影管理器 + 检查点（内存）
	pm := projection.NewProjectionManager(nil, bus)
	pm.WithCheckpointStore(projection.NewMemoryCheckpointStore())

	// 注册投影
	p, err := proj.NewOrderAmountProjection()
	must(err)
	must(pm.RegisterProjection(p))
	must(pm.StartProjection(p.GetName()))

	// 发布事件
	for i := 1; i <= 3; i++ {
		evt := eventing.NewDomainEvent(1000, "Order", "OrderCreated", uint64(i), &proj.OrderCreated{Amount: i * 10})
		must(bus.PublishEvent(ctx, evt))
	}
	time.Sleep(200 * time.Millisecond)

	// 查看投影状态与读模型
	st, err := pm.GetProjectionStatus(p.GetName())
	must(err)
	fmt.Printf("Projection=%s Processed=%d TotalAmount=%d\n", st.Name, st.ProcessedEvents, proj.GetTotal())

	// 模拟“重启后追赶”
	must(pm.StopProjection(p.GetName()))
	must(pm.ResumeFromCheckpoint(ctx, p.GetName()))
	for i := 4; i <= 5; i++ {
		evt := eventing.NewDomainEvent(1000, "Order", "OrderCreated", uint64(i), &proj.OrderCreated{Amount: i * 10})
		must(bus.PublishEvent(ctx, evt))
	}
	time.Sleep(200 * time.Millisecond)
	st, err = pm.GetProjectionStatus(p.GetName())
	must(err)
	fmt.Printf("After resume -> Processed=%d TotalAmount=%d\n", st.ProcessedEvents, proj.GetTotal())
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
