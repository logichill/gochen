# DDD / Event Sourcing / CQRS 速查

事件溯源写端通过聚合应用事件并保存，读端可通过 Projection 更新独立读模型。Core 提供内存实现，SQL、Host 和 REST 装配在 Runtime。

## 组件链路

| 环节 | 入口 |
| --- | --- |
| 领域事件 | `domain.IDomainEvent.EventType()` |
| 聚合 | `domain/eventsourced.EventSourcedAggregate`、MetadataRegistry |
| 领域事件持久化适配 | `app/eventsourced.NewDomainEventStore` |
| 仓储 | `app/eventsourced.NewEventSourcedRepository` |
| 命令服务 | `app/eventsourced.NewEventSourcedService` |
| 事件事实 | `eventing/store.IEventStreamStore` |
| 可靠发布 | `eventing/outbox` → `eventing/bus` |
| 读模型 | `eventing/projection` |

## 内存事件溯源示例

下例可在依赖 Core 的模块中运行，演示构造、保存和回放聚合：

```go
package main

import (
	"context"
	"fmt"
	"log"

	appes "gochen/app/eventsourced"
	domaines "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/gen"
)

type Deposited struct {
	Amount int64 `json:"amount"`
}

func (*Deposited) EventType() string { return "MoneyDeposited" }

type Account struct {
	*domaines.EventSourcedAggregate[int64]
	Balance int64
}

func (a *Account) ApplyDeposited(event *Deposited) { a.Balance += event.Amount }

func (a *Account) Deposit(amount int64) error {
	if amount <= 0 {
		return errors.NewCode(errors.Validation, "amount must be positive")
	}
	return a.ApplyAndRecord(&Deposited{Amount: amount})
}

func run() error {
	ctx := context.Background()
	metadata := domaines.NewMetadataRegistry()
	reg := registry.NewRegistry()
	if err := reg.Register("MoneyDeposited", func() any { return &Deposited{} }); err != nil {
		return err
	}
	domainStore, err := appes.NewDomainEventStore(appes.DomainEventStoreOptions[*Account, int64]{
		AggregateType:    "Account",
		EventIDGenerator: gen.NewUUIDGenerator(),
		EventStore:       store.NewMemoryEventStore[int64](),
		EventRegistry:    reg,
		UpgraderRegistry: upcast.NewUpgraderRegistry(),
	})
	if err != nil {
		return err
	}
	repo, err := appes.NewEventSourcedRepository(appes.RepositoryOptions[*Account, int64]{
		AggregateType:    "Account",
		Sample:           &Account{},
		Store:            domainStore,
		MetadataRegistry: metadata,
		Factory: func(id int64) (*Account, error) {
			account := &Account{}
			aggregate, err := domaines.InitAggregate(metadata, account, id, "Account")
			if err != nil {
				return nil, err
			}
			account.EventSourcedAggregate = aggregate
			return account, nil
		},
	})
	if err != nil {
		return err
	}
	account, err := repo.GetOrCreate(ctx, 1)
	if err != nil {
		return err
	}
	if err := account.Deposit(100); err != nil {
		return err
	}
	if err := repo.Save(ctx, account); err != nil {
		return err
	}
	restored, err := repo.Get(ctx, 1)
	if err != nil {
		return err
	}
	fmt.Println(restored.Balance) // 100
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
```

MetadataRegistry 按 Go 事件类型预编译 handler。处理方法须导出、事件参数为指针；同一类型不能有多个 handler。保存基线使用聚合的 GetExpectedVersion，聚合身份是 aggregate type 与 ID 的组合。

## 命令、发布与读模型

`EventSourcedService` 编排加载聚合、执行命令和保存。ConcurrencyRetry 可在保存发生并发冲突时重新执行，handler 必须可重入；最终 Hook 用于接收整个调用的结果。需要 Operation 时使用 `ExecuteCommandWithOperation` 或 `ExecuteCommandWithResolvedOperation`，装配要求见 [Operation](../../app/operation/README.md#应用层接入)。

`CommandBus.Dispatch` 表示消息投递；`CommandExecutor.Execute` 表示本地同步执行。需要即时业务结果或 Saga 补偿判断时使用执行端口，不能依赖异步投递成功。

启用可靠发布时，把 OutboxRepo 注入 DomainEventStore，在同一原子边界保存事件与 Outbox，再由 Publisher 发布。直接写 Store 后发布总线不提供这两个动作的原子性。

Projection 按事件更新读模型；启用 checkpoint 需可扫描的事件 Store，并让读模型与 checkpoint 同事务保存。在线处理、恢复、重建的串行边界由 ProjectionManager 管理。

## 进一步阅读

- [EventStore](../../eventing/store/README.md)、[Outbox](../../eventing/outbox/README.md)、[Projection](../../eventing/projection/README.md)。
- [事件 Schema 与回放](event-schema-evolution.md)、[数据库 Schema 与迁移](../guides/db-schema-migration-guide.md)。
- Runtime 示例：`gochen-runtime` 仓库 `examples/domain/eventsourced`、`examples/infra/outbox/sql`、`examples/infra/projection/sql_checkpoint`、`examples/infra/snapshot/basic`。
