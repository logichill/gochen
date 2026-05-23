package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"gochen/app/eventsourced"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing"
	"gochen/eventing/outbox"
	"gochen/eventing/registry"
	estore "gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/messaging"
)

// 领域事件
type Deposited struct{ Amount int }

// EventType 返回入金事件的类型标识。
func (e *Deposited) EventType() string { return "Deposited" }

// Wallet 演示接入 Outbox 的事件溯源钱包聚合。
type Wallet struct {
	*deventsourced.EventSourcedAggregate[int64] `aggregate:"Wallet"`
	Balance                                     int
}

// NewWallet 创建一个新的钱包聚合。
func NewWallet(metadataRegistry *deventsourced.MetadataRegistry, id int64) *Wallet {
	wallet, err := deventsourced.New[Wallet, int64](metadataRegistry, id)
	if err != nil {
		panic(err)
	}
	return wallet
}

// ApplyDeposited 把入金金额累加到钱包余额。
func (a *Wallet) ApplyDeposited(evt *Deposited) {
	a.Balance += evt.Amount
}

// mockOutbox 仅打印 SaveWithEvents，演示 API 用法（生产可替换为 SQL 实现）
type mockOutbox struct{}

// SaveWithEvents 直接打印待入 Outbox 的事件，便于观察写入结果。
func (m *mockOutbox) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event[int64]) error {
	log.Printf("[outbox] aggregate=%d, events=%d", aggregateID, len(events))
	for _, e := range events {
		log.Printf("  -> id=%s type=%s v=%d payload=%#v", e.GetID(), e.GetType(), e.GetVersion(), messaging.PayloadValue(e.GetPayload()))
	}
	return nil
}

// ClaimPendingEntries 在示例实现中始终返回空结果，因为事件会被立即打印而不排队。
func (m *mockOutbox) ClaimPendingEntries(ctx context.Context, limit int) ([]outbox.OutboxEntry[int64], error) {
	return nil, nil
}

// MarkAsPublished 在示例实现中保留为空操作。
func (m *mockOutbox) MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error {
	return nil
}

// MarkAsFailed 在示例实现中保留为空操作。
func (m *mockOutbox) MarkAsFailed(ctx context.Context, entryID int64, claimToken string, errorMsg string, nextRetryAt time.Time) error {
	return nil
}

// RenewClaim 在示例实现中保留为空操作。
func (m *mockOutbox) RenewClaim(ctx context.Context, entryID int64, claimToken string) error {
	return nil
}

// DeletePublished 在示例实现中不维护已发布记录，因此无需清理。
func (m *mockOutbox) DeletePublished(ctx context.Context, olderThan time.Time) error { return nil }

// main 演示事件溯源仓储与自定义 Outbox 适配器的组合方式。
func main() {
	log.SetPrefix("[es_outbox] ")
	ctx := context.Background()
	metadataRegistry := deventsourced.NewMetadataRegistry()
	mustMetadata(metadataRegistry.Register(&Wallet{}, "Wallet"))
	reg := registry.NewRegistry()
	if err := reg.Register("Deposited", func() any { return &Deposited{} }); err != nil {
		log.Fatal(err)
	}
	upgraders := upcast.NewUpgraderRegistry()

	// 基础 ES 仓储（内存事件存储）
	storeAdapter, err := eventsourced.NewDomainEventStore(eventsourced.DomainEventStoreOptions[*Wallet, int64]{
		AggregateType:    "Wallet",
		EventStore:       estore.NewMemoryEventStore(),
		OutboxRepo:       &mockOutbox{},
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	if err != nil {
		log.Fatal(err)
	}

	repo, err := eventsourced.NewEventSourcedRepository(eventsourced.RepositoryOptions[*Wallet, int64]{
		AggregateType:    "Wallet",
		Sample:           &Wallet{},
		Factory:          func(id int64) (*Wallet, error) { return NewWallet(metadataRegistry, id), nil },
		Store:            storeAdapter,
		MetadataRegistry: metadataRegistry,
	})
	if err != nil {
		log.Fatal(err)
	}

	// 构造聚合与事件
	w := NewWallet(metadataRegistry, 7001)
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
	loaded, err := repo.Get(ctx, 7001)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Wallet[%d] Balance=%d Version=%d\n", loaded.GetID(), loaded.Balance, loaded.GetVersion())
}

func mustMetadata(_ *deventsourced.Metadata, err error) {
	if err != nil {
		log.Fatal(err)
	}
}
