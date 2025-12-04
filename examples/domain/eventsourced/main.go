package main

import (
	"context"
	"fmt"
	"log"

	"gochen/app/eventsourced"
	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing"
	ebus "gochen/eventing/bus"
	estore "gochen/eventing/store"
	"gochen/messaging"
	mtransport "gochen/messaging/transport/memory"
)

// 领域事件载荷
type AccountOpened struct{ Initial int }

func (e *AccountOpened) EventType() string { return "AccountOpened" }

type Deposited struct{ Amount int }

func (e *Deposited) EventType() string { return "Deposited" }

type Withdrawn struct{ Amount int }

func (e *Withdrawn) EventType() string { return "Withdrawn" }

// 聚合
type Account struct {
	*deventsourced.EventSourcedAggregate[int64]
	Balance int
}

func NewAccount(id int64) *Account {
	return &Account{EventSourcedAggregate: deventsourced.NewEventSourcedAggregate[int64](id, "account")}
}

func (a *Account) Open(initial int) error {
	if a.GetVersion() != 0 {
		return fmt.Errorf("already opened")
	}
	return a.ApplyAndRecord(&AccountOpened{Initial: initial})
}
func (a *Account) Deposit(n int) error {
	return a.ApplyAndRecord(&Deposited{Amount: n})
}
func (a *Account) Withdraw(n int) error {
	if a.Balance < n {
		return fmt.Errorf("insufficient")
	}
	return a.ApplyAndRecord(&Withdrawn{Amount: n})
}

func (a *Account) ApplyEvent(evt domain.IDomainEvent) error {
	switch e := evt.(type) {
	case *AccountOpened:
		a.Balance = e.Initial
	case *Deposited:
		a.Balance += e.Amount
	case *Withdrawn:
		a.Balance -= e.Amount
	}
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

func main() {
	log.Println("=== Event Sourcing Demo ===")

	// 事件存储 + 总线（内存）
	store := estore.NewMemoryEventStore()
	bus := ebus.NewEventBus(messaging.NewMessageBus(mtransport.NewMemoryTransport(1000, 2)))

	// 构造 IEventStore 实现
	eventStoreAdapter, err := eventsourced.NewDomainEventStore[*Account](eventsourced.DomainEventStoreOptions[*Account]{
		AggregateType: "account",
		Factory:       NewAccount,
		EventStore:    store,
		EventBus:      bus,
		PublishEvents: true,
	})
	if err != nil {
		panic(err)
	}

	// 构造领域层仓储
	repo, err := deventsourced.NewEventSourcedRepository[*Account]("account", NewAccount, eventStoreAdapter)
	if err != nil {
		panic(err)
	}

	// 订阅打印事件
	_ = bus.SubscribeEvent(context.Background(), "*", ebus.EventHandlerFunc(func(ctx context.Context, evt eventing.IEvent) error {
		log.Printf("事件: %s v%d -> agg=%d", evt.GetType(), evt.GetVersion(), evt.GetAggregateID())
		return nil
	}))

	// 打开并操作账户
	acc := NewAccount(1001)
	_ = acc.Open(100)
	_ = acc.Deposit(50)
	_ = repo.Save(context.Background(), acc)

	// 追加更多事件
	_ = acc.Withdraw(30)
	_ = repo.Save(context.Background(), acc)

	// 重建读取
	loaded, _ := repo.GetByID(context.Background(), 1001)
	log.Printf("账户余额: %d, 版本: %d", loaded.Balance, loaded.GetVersion())
}
