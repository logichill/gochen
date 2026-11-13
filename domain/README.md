# domain：领域建模契约

`domain` 是业务领域可复用的共享内核，提供实体、聚合、仓储端口与领域约束。非测试代码只依赖标准库、`gochen/errors` 和自身子树。

## 建模方式

| 子包 | 内容 | 场景 |
| --- | --- | --- |
| `domain/crud` | `Entity`、条件仓储、迁移与批量 / 清洗接口 | 一般管理数据 |
| `domain/meta` / `domain/spec` | 开放维度、类型化引用、转换和条件解释器 | 实体与历史快照共用的条件 |
| `domain.ChangeInfo` / `domain.DeletionState` | 独立修改归因与删除状态组件 | 按需组合，不要求审计存储 |
| `domain/eventsourced` | 聚合基类、metadata、领域事件存储与仓储端口 | 通过事件流重建业务状态 |

## 实体与仓储

- `IEntity[ID]` 要求 `GetID()` 和 `GetVersion()`；`ISettableID` 提供可选 ID 回填能力。
- `IValidatable.Validate()` 表达不依赖外部资源的领域不变量。需要数据库或上下文的校验通过应用层注入。
- `ITimestamps` 与 `ISoftDeletable` 是独立能力；带 operator 的软删除由独立 `domain.ISoftDeletableBy` 补充。
- `domain/crud.IRepository` 只有 `Create`、`Update`、`Delete`、`Get`。`IBulkOperations`、`IPurgeRepository` 等按需实现；列表与计数走 `app/query.IQueryableRepository`，只读应用入口为 `app/crud.IReadRepository`。

仓储错误通过 `gochen/errors` 表达；基础仓储的 Delete 可采用幂等或严格未命中语义，具体实现须明确。领域查询优先使用有业务含义的方法；`app/query` 是适配器查询协议，不属于领域层依赖。

实体以 `entity:"id"` 标记标识，以 `dim:"..."` 声明项目维度，以 `managed:"true"` 声明托管字段。`meta.Ref` 通过 ID、ByField、ByDimension 构造，字段和维度别名统一解析。维度支持标量及一层标量指针。

`auth.TenantEntity` 提供租户便利类型，隔离在 Runtime Repo 构造期显式声明。全部维度默认受普通更新保护，mutable / assignable / transferable 由资源契约配置；独立迁移通过 `ITransferRepository` 执行，见 [资源契约](../docs/architecture/resource-contracts.md)。

## 实体状态复制

`meta.Clone` 为内存仓储、业务状态比较和展示钩子建立独立状态。公开的指针、map、slice、数组和循环引用可以复制；私有标量及 `time.Time` 的不可变资料保留。

非空私有可变引用，以及非空 channel、函数、unsafe pointer 返回 `Unsupported`。nil 引用合法。实体采用这些形状时，应明确其持久化和复制方式，再接入需要独立快照的用例。

## 审计与软删除

`domain/crud.TrackedEntity` 包含 ID、Version、创建 / 更新 / 删除时间及操作人。`SetUpdatedInfo` 设置更新人和时间，保留当前版本；`SoftDeleteBy` 要求非空 operator，`Restore` 清除删除信息。条件仓储依据写入前版本统一计算下一版本，覆盖普通更新、软删、恢复、Transfer 与批量更新。物理 Delete/Purge 校验旧版本。普通实体显式使用 `WithoutVersioning` 时沿用退出版本保护的规则；带 IChangeInfo 的实体不可退出版本保护，纯删除状态沿用仓储显式 WithoutVersioning 的既有规则。创建时带 IChangeInfo 的实体初始版本归零，普通实体保留既有初始版本规则。

`audit.IAuditStore` 和 `audit.IAuditQueryStore` 定义独立写入与查询契约，位于 gochen/audit，不是领域实体的必要能力。包含已删读取与已删除列表由 domain/crud 的 IDeletedReadRepository、IDeletedQueryRepository 表达。事务审计由 CRUD 的可选 Audit 配置参与，历史读取由 app/crud/audit.Reader 提供，删除生命周期统一由 app/crud.Application 提供；恢复写入独立使用 IRestoreRepository，Purge 无需恢复能力。

`AuditRecord.Dimensions` 保存全部维度的不可变标量快照，`OperationID` 关联一次逻辑操作。审计读取通过同一解释器对历史事实求值，缺键明确失败，实体物理删除后仍可查询。迁移两侧各保存自己的前后像快照。

## 事件溯源

`IDomainEvent` 只要求稳定的 `EventType()`。显式 `Model[S,E]` 由 Initial、Clone、Reduce 组成，Replay 不需要 registry 或聚合基类。Clone 必须对可变引用做真正隔离，事件按不可变输入使用。应用 StateRepository 持有 ID 与版本，复用原事件存储和 codec。

可选便利路径：`EventSourcedAggregate` 管理版本与未提交事件，`ApplyAndRecord` 应用并记录事件，仓储以 `GetExpectedVersion()` 作为保存基线。

组合根显式创建 `MetadataRegistry`，可通过 `RegisterSet`、`Aggregate`、`RegisterModuleAggregates` 预热；`InitAggregate` / `New` 构造时也可按该 registry 确保 metadata 已就绪。

反射事件路由要求处理方法导出、事件参数为指针类型，同一 Go 事件类型只能映射一个处理方法。未匹配 handler 时返回错误，不能静默推进聚合版本。

领域侧存储端口为 `IDomainEventStore`，默认持久化适配和命令编排在 `app/eventsourced`。示例见[事件溯源速查](../docs/reference/ddd-eventsourcing-quick-reference.md)，存储事件的版本处理见[事件 Schema 与回放](../docs/reference/event-schema-evolution.md)。
