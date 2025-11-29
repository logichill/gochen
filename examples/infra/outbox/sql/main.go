package main

import (
	"context"
	"fmt"
	"log"

	_ "modernc.org/sqlite"

	dbcore "gochen/data/db"
	basicdb "gochen/data/db/basic"
	sentity "gochen/domain/entity"
	"gochen/domain/eventsourced"
	"gochen/eventing"
	ebus "gochen/eventing/bus"
	"gochen/eventing/outbox"
	sqlstore "gochen/eventing/store/sql"
	"gochen/examples/infra/outbox/sql/internal/schema"
	"gochen/messaging"
	mtransport "gochen/messaging/transport/memory"
)

// 领域事件
type Opened struct{ Initial int }
type Deposited struct{ Amount int }

// 聚合
type Account struct {
	*sentity.EventSourcedAggregate[int64]
	Balance int
}

func NewAccount(id int64) *Account {
	return &Account{EventSourcedAggregate: sentity.NewEventSourcedAggregate[int64](id, "Account")}
}

func (a *Account) ApplyEvent(evt eventing.IEvent) error {
	switch p := evt.GetPayload().(type) {
	case *Opened:
		a.Balance = p.Initial
	case *Deposited:
		a.Balance += p.Amount
	}
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

func main() {
	log.SetPrefix("[sql_outbox_demo] ")
	ctx := context.Background()

	// 1) 初始化 SQLite DB 与表结构
	db := mustNewDB(dbcore.DBConfig{Driver: "sqlite", Database: ":memory:"})
	must(schema.EnsureEventAndOutboxTables(ctx, db))

	// 2) 构建 SQL EventStore 与 Outbox Repository
	es := sqlstore.NewSQLEventStore(db, "event_store")
	ob := outbox.NewSimpleSQLOutboxRepository(db, es, nil)

	// 3) 基础 ES 仓储 + Outbox 装饰器
	base, err := eventsourced.NewEventSourcedRepository[*Account](eventsourced.EventSourcedRepositoryOptions[*Account]{
		AggregateType: "Account",
		Factory:       NewAccount,
		EventStore:    es,
	})
	must(err)
	repo, err := eventsourced.NewOutboxAwareRepository(base, ob)
	must(err)

	// 4) 产生并保存事件（原子写入事件与 Outbox）
	acc := NewAccount(9001)
	_ = acc.ApplyAndRecord(eventing.NewDomainEvent(acc.GetID(), acc.GetAggregateType(), "Opened", uint64(acc.GetVersion()+1), &Opened{Initial: 100}))
	_ = acc.ApplyAndRecord(eventing.NewDomainEvent(acc.GetID(), acc.GetAggregateType(), "Deposited", uint64(acc.GetVersion()+1), &Deposited{Amount: 50}))
	must(repo.Save(ctx, acc))

	// 5) 创建事件总线 + Publisher，一次性发布 Outbox 待处理记录
	bus := ebus.NewEventBus(messaging.NewMessageBus(mtransport.NewMemoryTransport(1024, 2)))
	// 订阅打印发布的事件
	_ = bus.SubscribeEvent(ctx, "*", ebus.EventHandlerFunc(func(ctx context.Context, evt eventing.IEvent) error {
		log.Printf("published event: %s v%d -> agg=%d", evt.GetType(), evt.GetVersion(), evt.GetAggregateID())
		return nil
	}))
	cfg := outbox.DefaultOutboxConfig()
	cfg.BatchSize = 100
	publisher := outbox.NewPublisher(ob, bus, cfg, nil)
	must(publisher.PublishPending(ctx))

	// 6) 检查 Outbox 待发布数量（应变为 0）
	pending, err := ob.GetPendingEntries(ctx, 10)
	must(err)
	fmt.Printf("outbox pending(after publish)=%d\n", len(pending))

	// 7) 重放读取聚合状态
	loaded, err := base.GetByID(ctx, 9001)
	must(err)
	fmt.Printf("Account[%d] Balance=%d Version=%d\n", loaded.GetID(), loaded.Balance, loaded.GetVersion())
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
