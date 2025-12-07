package main

import (
	"context"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"

	"gochen/app/eventsourced"
	dbcore "gochen/data/db"
	basicdb "gochen/data/db/basic"
	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	estore "gochen/eventing/store"
	ssnap "gochen/eventing/store/snapshot"
)

// Counter 事件载荷
type ValueSet struct{ V int }

func (e *ValueSet) EventType() string { return "ValueSet" }

// Counter 聚合
type Counter struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

func NewCounter(id int64) *Counter {
	return &Counter{EventSourcedAggregate: deventsourced.NewEventSourcedAggregate[int64](id, "Counter")}
}

func (c *Counter) ApplyEvent(evt domain.IDomainEvent) error {
	if p, ok := evt.(*ValueSet); ok {
		c.Value = p.V
	}
	return c.EventSourcedAggregate.ApplyEvent(evt)
}

func main() {
	log.SetPrefix("[snapshot_demo] ")
	ctx := context.Background()

	// 1) 初始化 SQLite + SQL EventStore + SQL SnapshotStore
	db := mustNewDB(dbcore.DBConfig{Driver: "sqlite", Database: ":memory:"})
	eventStore := estore.NewMemoryEventStore() // 示例中事件仍用内存存储，快照落 SQLite 即可
	snapStore := ssnap.NewSQLStore(db, "event_snapshots")
	snapCfg := ssnap.DefaultConfig()
	snapCfg.Frequency = 3 // 每 3 个事件建议创建一次快照
	snapMgr := ssnap.NewManager[int64](snapStore, snapCfg)

	// 2) 构建 ES 仓储
	storeAdapter, err := eventsourced.NewDomainEventStore(eventsourced.DomainEventStoreOptions[*Counter, int64]{
		AggregateType:   "Counter",
		Factory:         NewCounter,
		EventStore:      eventStore,
		SnapshotManager: snapMgr,
	})
	must(err)

	repo, err := deventsourced.NewEventSourcedRepository[*Counter]("Counter", NewCounter, storeAdapter)
	must(err)

	aggID := int64(1001)
	// 3) 第一次：从头重放，不使用快照
	start := time.Now()
	createAndSave(ctx, repo, aggID, 5) // 写入 5 个事件
	firstLoaded, err := repo.GetByID(ctx, aggID)
	must(err)
	firstDuration := time.Since(start)
	fmt.Printf("First load: value=%d version=%d duration=%s (no snapshot)\n", firstLoaded.Value, firstLoaded.GetVersion(), firstDuration)

	// 4) 为当前聚合创建快照（示例中直接调用 Manager）
	_ = snapMgr.CreateSnapshot(ctx, aggID, "Counter", firstLoaded, firstLoaded.GetVersion())

	// 5) 第二次：先从快照恢复，再重放增量事件
	// 模拟“应用重启”：新仓储实例 + 从快照恢复
	// 这里直接手动调用 LoadSnapshot 演示：实际可在仓储层集成
	start = time.Now()
	// 加 3 条新增事件（版本 6~8）
	createAndSave(ctx, repo, aggID, 3)

	// 从快照加载聚合状态
	loadedFromSnap := NewCounter(aggID)
	if snap, err := snapMgr.LoadSnapshot(ctx, aggID, loadedFromSnap); err == nil {
		// 从快照版本之后继续重放
		events, err := eventStore.LoadEvents(ctx, aggID, snap.Version)
		must(err)
		for i := range events {
			evt := events[i]
			if de, ok := evt.GetPayload().(domain.IDomainEvent); ok {
				_ = loadedFromSnap.ApplyEvent(de)
			}
		}
	} else {
		log.Printf("load snapshot failed, fallback to full replay: %v", err)
		// 回退方案：从头重放
		events, err := eventStore.LoadEvents(ctx, aggID, 0)
		must(err)
		for i := range events {
			evt := events[i]
			if de, ok := evt.GetPayload().(domain.IDomainEvent); ok {
				_ = loadedFromSnap.ApplyEvent(de)
			}
		}
	}
	secondDuration := time.Since(start)
	fmt.Printf("Second load: value=%d version=%d duration=%s (with snapshot)\n", loadedFromSnap.Value, loadedFromSnap.GetVersion(), secondDuration)
}

// createAndSave 追加 n 个 ValueSet 事件并保存
func createAndSave(ctx context.Context, repo deventsourced.IEventSourcedRepository[*Counter, int64], id int64, n int) {
	agg, _ := repo.GetByID(ctx, id)
	if agg == nil {
		agg = NewCounter(id)
	}
	for i := 0; i < n; i++ {
		v := agg.Value + 1
		_ = agg.ApplyAndRecord(&ValueSet{V: v})
	}
	must(repo.Save(ctx, agg))
}

func mustNewDB(cfg dbcore.DBConfig) dbcore.IDatabase {
	db, err := basicdb.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	return db
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
