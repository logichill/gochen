# DDD / Event Sourcing / CQRS 速查

事件溯源写端通过聚合应用事件并保存，读端可通过 Projection 更新独立读模型。Core 提供内存实现，SQL、Host 和 REST 装配在 Runtime。

## 组件链路

| 环节 | 入口 |
| --- | --- |
| 领域事件 | `domain.IDomainEvent.EventType()` |
| 聚合 | `domain/eventsourced.EventSourcedAggregate`、MetadataRegistry |
| 领域事件持久化适配 | `app/eventsourced/eventstore.NewDomainEventStore` |
| 仓储 | `app/eventsourced.NewEventSourcedRepository` |
| 应用入口 | `app/eventsourced.NewApplication` |
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
	esstore "gochen/app/eventsourced/eventstore"
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
	*domaines.EventSourcedAggregate[int64] `aggregate:"Account"`
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
	if err := metadata.RegisterSet(domaines.AggregateFromTag(&Account{})); err != nil {
		return err
	}
	reg := registry.NewRegistry()
	if err := reg.Register("MoneyDeposited", func() any { return &Deposited{} }); err != nil {
		return err
	}
	domainStore, err := esstore.NewDomainEventStore(esstore.DomainEventStoreOptions[int64]{
		PublishMode:     esstore.PublishModePersistOnly,
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
			return domaines.New[Account, int64](metadata, id)
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

MetadataRegistry 按 Go 事件类型缓存 handler；运行时仍使用反射调用。处理方法命名为 `ApplyXxx`，恰好接收一个实现 `IDomainEvent` 的指针参数，只允许无返回值或单个 `error` 返回值；同一类型不能有多个 handler。不合法的候选方法在注册时失败，其他名称的方法不参与路由。保存基线使用聚合的 GetExpectedVersion，聚合身份是 aggregate type 与 ID 的组合。

业务只能通过命令方法产生变更，不能直接调用 `ApplyXxx` 或修改业务字段。`ApplyEvent` 供回放使用，不去重；处理器须确定且无外部副作用，应用失败必须丢弃实例。接入约束和回放验证见[聚合使用指南](../../domain/eventsourced/README.md)。

## 命令、发布与读模型

`Application` 编排加载聚合、执行命令和保存：`Get` 按 read 规约回放校验，`Execute` 修改已有聚合并在保存冲突时重试（命令函数必须可重入），`ExecuteNew` 新建聚合不做重试，配置 `History` 后 `AuditTrail` 读取事件历史。授权规约在内存中匹配回放后的聚合；操作人、动作、原因与关联 ID 写入事件元数据。命令总线接入使用 `AsExecuteHandler` / `AsExecuteNewHandler`。需要统一信封或延迟收敛跟踪时由传输层使用 Operation 包装应用调用，不再内嵌于事件溯源应用。

历史条目、分页模型及 helper 位于 `app/eventsourced`，分页直接使用 `store.IAggregateEventReader`。接口和授权边界见 [事件历史](../../app/eventsourced/README.md#事件历史)。

`CommandBus.Dispatch` 表示消息投递；`CommandExecutor.Execute` 表示本地同步执行。需要即时业务结果或 Saga 补偿判断时使用执行端口，不能依赖异步投递成功。

启用可靠发布时，把 OutboxRepo 注入 DomainEventStore，在同一原子边界保存事件与 Outbox，再由 Publisher 发布。直接写 Store 后发布总线不提供这两个动作的原子性。

Projection 按事件更新读模型；启用 checkpoint 需可扫描的事件 Store，并让读模型与 checkpoint 同事务保存。在线处理、恢复、重建的串行边界由 ProjectionManager 管理。

## 进一步阅读

- [EventStore](../../eventing/store/README.md)、[Outbox](../../eventing/outbox/README.md)、[Projection](../../eventing/projection/README.md)。
- [事件 Schema 与回放](event-schema-evolution.md)、[数据库 Schema 与迁移](../guides/db-schema-migration-guide.md)。
- Runtime 示例：`gochen-runtime` 仓库 `examples/domain/eventsourced`、`examples/infra/outbox/sql`、`examples/infra/projection/sql_checkpoint`、`examples/infra/snapshot/basic`。
