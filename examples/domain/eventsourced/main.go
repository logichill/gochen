package main

import (
	"context"
	"fmt"
	"log"

	"gochen/domain/eventsourced"
	"gochen/eventing"
	ebus "gochen/eventing/bus"
	estore "gochen/eventing/store"
	"gochen/messaging"
	mtransport "gochen/messaging/transport/memory"
)

// 事件载荷
type AccountOpened struct{ Initial int }
type Deposited struct{ Amount int }
type Withdrawn struct{ Amount int }

// 聚合
type Account struct {
	*eventsourced.EventSourcedAggregate[int64]
	Balance int
}

func NewAccount(id int64) *Account {
	return &Account{EventSourcedAggregate: eventsourced.NewEventSourcedAggregate[int64](id, "account")}
}

func (a *Account) Open(initial int) error {
	if a.GetVersion() != 0 {
		return fmt.Errorf("already opened")
	}
	evt := eventing.NewDomainEvent(a.GetID(), a.GetAggregateType(), "AccountOpened", uint64(a.GetVersion()+1), &AccountOpened{Initial: initial})
	return a.ApplyAndRecord(evt)
}
func (a *Account) Deposit(n int) error {
	evt := eventing.NewDomainEvent(a.GetID(), a.GetAggregateType(), "Deposited", uint64(a.GetVersion()+1), &Deposited{Amount: n})
	return a.ApplyAndRecord(evt)
}
func (a *Account) Withdraw(n int) error {
	if a.Balance < n {
		return fmt.Errorf("insufficient")
	}
	evt := eventing.NewDomainEvent(a.GetID(), a.GetAggregateType(), "Withdrawn", uint64(a.GetVersion()+1), &Withdrawn{Amount: n})
	return a.ApplyAndRecord(evt)
}

func (a *Account) ApplyEvent(evt eventing.IEvent) error {
	switch e := evt.GetPayload().(type) {
	case *AccountOpened:
		a.Balance = e.Initial
	case *Deposited:
		a.Balance += e.Amount
	case *Withdrawn:
		a.Balance -= e.Amount
	}
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

func (a *Account) When(evt eventing.IEvent) error { return a.ApplyEvent(evt) }

func (a *Account) LoadFromHistory(events []eventing.IEvent) error {
	for _, evt := range events {
		if err := a.ApplyEvent(evt); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	log.Println("=== Event Sourcing Demo ===")

	// 事件存储 + 总线（内存）
	store := estore.NewMemoryEventStore()
	bus := ebus.NewEventBus(messaging.NewMessageBus(mtransport.NewMemoryTransport(1000, 2)))

	// 构造仓储模板
	repo, err := eventsourced.NewEventSourcedRepository[*Account](eventsourced.EventSourcedRepositoryOptions[*Account]{
		AggregateType: "account",
		Factory:       func(id int64) *Account { return NewAccount(id) },
		EventStore:    store,
		EventBus:      bus,
		PublishEvents: true,
	})
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
