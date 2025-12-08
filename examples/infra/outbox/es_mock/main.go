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
	"gochen/eventing/outbox"
	estore "gochen/eventing/store"
)

// 领域事件
type Deposited struct{ Amount int }

func (e *Deposited) EventType() string { return "Deposited" }

// 聚合
type Wallet struct {
	*deventsourced.EventSourcedAggregate[int64]
	Balance int
}

func NewWallet(id int64) *Wallet {
	return &Wallet{EventSourcedAggregate: deventsourced.NewEventSourcedAggregate[int64](id, "Wallet")}
}

func (a *Wallet) ApplyEvent(evt domain.IDomainEvent) error {
	switch p := evt.(type) {
	case *Deposited:
		a.Balance += p.Amount
	}
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

// mockOutbox 仅打印 SaveWithEvents，演示 API 用法（生产可替换为 SQL 实现）
type mockOutbox struct{}

func (m *mockOutbox) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event[int64]) error {
	log.Printf("[outbox] aggregate=%d, events=%d", aggregateID, len(events))
	for _, e := range events {
		log.Printf("  -> id=%s type=%s v=%d payload=%#v", e.GetID(), e.GetType(), e.GetVersion(), e.GetPayload())
	}
	return nil
}

func (m *mockOutbox) GetPendingEntries(ctx context.Context, limit int) ([]outbox.OutboxEntry, error) {
	return nil, nil
}
func (m *mockOutbox) MarkAsPublished(ctx context.Context, entryID int64) error { return nil }
func (m *mockOutbox) MarkAsFailed(ctx context.Context, entryID int64, errorMsg string, nextRetryAt time.Time) error {
	return nil
}
func (m *mockOutbox) DeletePublished(ctx context.Context, olderThan time.Time) error { return nil }

func main() {
	log.SetPrefix("[es_outbox] ")
	ctx := context.Background()

	// 基础 ES 仓储（内存事件存储）
	storeAdapter, err := eventsourced.NewDomainEventStore(eventsourced.DomainEventStoreOptions[*Wallet, int64]{
		AggregateType: "Wallet",
		Factory:       NewWallet,
		EventStore:    estore.NewMemoryEventStore(),
		OutboxRepo:    &mockOutbox{},
	})
	if err != nil {
		log.Fatal(err)
	}

	repo, err := deventsourced.NewEventSourcedRepository[*Wallet]("Wallet", NewWallet, storeAdapter)
	if err != nil {
		log.Fatal(err)
	}

	// 构造聚合与事件
	w := NewWallet(7001)
	if err := w.ApplyAndRecord(&Deposited{Amount: 100}); err != nil {
		log.Fatal(err)
	}
	if err := w.ApplyAndRecord(&Deposited{Amount: 50}); err != nil {
		log.Fatal(err)
	}

	// 保存：同事务写事件与 Outbox（示例为 mock 打印）
	if err := repo.Save(ctx, w); err != nil {
		log.Fatal(err)
	}

	// 重放读取
	loaded, err := repo.GetByID(ctx, 7001)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Wallet[%d] Balance=%d Version=%d\n", loaded.GetID(), loaded.Balance, loaded.GetVersion())
}
