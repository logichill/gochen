package main

import (
	"context"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"

	dbcore "gochen/data/db"
	basicdb "gochen/data/db/basic"
	"gochen/eventing"
	ebus "gochen/eventing/bus"
	"gochen/eventing/projection"
	"gochen/examples/infra/projection/sql_checkpoint/internal/checkpoint"
	"gochen/examples/infra/projection/sql_checkpoint/internal/proj"
	"gochen/messaging"
	mtransport "gochen/messaging/transport/memory"
)

func main() {
	log.SetPrefix("[projection_sql_checkpoint] ")
	ctx := context.Background()

	// SQLite 文件数据库（便于在同进程内模拟“重启后”持久化）
	db := mustNewDB(dbcore.DBConfig{Driver: "sqlite", Database: "proj_cp_demo.db"})

	// 事件总线（内存）
	bus := ebus.NewEventBus(messaging.NewMessageBus(mtransport.NewMemoryTransport(1024, 2)))

	// 持久化检查点存储（SQLite示例实现）并确保表存在
	cp := checkpoint.NewSQLiteCheckpointStore(db, "projection_checkpoints")
	must(cp.EnsureTable(ctx))

	// 注册投影并启用检查点
	pm := projection.NewProjectionManager(nil, bus).WithCheckpointStore(cp)
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
	st, _ := pm.GetProjectionStatus(p.GetName())
	fmt.Printf("Before restart: Processed=%d Total=%d\n", st.ProcessedEvents, proj.GetTotal())

	// 模拟“重启”——重新创建管理器并从检查点恢复
	pm2 := projection.NewProjectionManager(nil, bus).WithCheckpointStore(cp)
	// 需要重新注册投影实例
	p2, err := proj.NewOrderAmountProjection()
	must(err)
	must(pm2.RegisterProjection(p2))
	must(pm2.ResumeFromCheckpoint(ctx, p2.GetName()))
	for i := 4; i <= 5; i++ {
		evt := eventing.NewDomainEvent(1000, "Order", "OrderCreated", uint64(i), &proj.OrderCreated{Amount: i * 10})
		must(bus.PublishEvent(ctx, evt))
	}
	time.Sleep(200 * time.Millisecond)
	st2, _ := pm2.GetProjectionStatus(p2.GetName())
	fmt.Printf("After restart: Processed=%d Total=%d\n", st2.ProcessedEvents, proj.GetTotal())
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func mustNewDB(cfg dbcore.DBConfig) dbcore.IDatabase {
	d, err := basicdb.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	return d
}
