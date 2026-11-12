# domain：领域建模契约

`domain` 是业务领域可复用的共享内核，提供实体、聚合、仓储端口与领域约束。非测试代码只依赖标准库、`gochen/errors` 和自身子树。

## 建模方式

| 子包 | 内容 | 场景 |
| --- | --- | --- |
| `domain/crud` | `Entity`、条件仓储、迁移与批量 / 清洗接口 | 一般管理数据 |
| `domain/meta` / `domain/spec` | 开放维度、类型化引用、转换和条件解释器 | 实体与历史快照共用的条件 |
| `domain/audited` | 审计字段、软删除、恢复和审计存储端口 | 需要审计轨迹的管理数据 |
| `domain/eventsourced` | 聚合基类、metadata、领域事件存储与仓储端口 | 通过事件流重建业务状态 |

## 实体与仓储

- `IEntity[ID]` 要求 `GetID()` 和 `GetVersion()`；`ISettableID` 提供可选 ID 回填能力。
- `IValidatable.Validate()` 表达不依赖外部资源的领域不变量。需要数据库或上下文的校验通过应用层注入。
- `ITimestamps` 与 `ISoftDeletable` 是独立能力；带 operator 的软删除由 `domain/audited` 补充。
- `domain/crud.IRepository` 只有 `Create`、`Update`、`Delete`、`Get`。`IBulkOperations`、`IPurgeRepository` 等按需实现；列表与计数走 `app/query.IQueryableRepository`，只读应用入口为 `app/crud.IReadRepository`。

仓储错误通过 `gochen/errors` 表达；基础仓储的 Delete 可采用幂等或严格未命中语义，具体实现须明确。领域查询优先使用有业务含义的方法；`app/query` 是适配器查询协议，不属于领域层依赖。

实体以 `entity:"id"` 标记标识，以 `dim:"..."` 声明项目维度，以 `managed:"true"` 声明托管字段。`meta.Ref` 通过 ID、ByField、ByDimension 构造，字段和维度别名统一解析。维度支持标量及一层标量指针。

`auth.TenantEntity` 提供租户便利类型，隔离在 Runtime Repo 构造期显式声明。全部维度默认受普通更新保护，mutable / assignable / transferable 由资源契约配置；独立迁移通过 `ITransferRepository` 执行，见 [资源契约](../docs/architecture/resource-contracts.md)。

## 实体状态复制

`meta.Clone` 为内存仓储、业务状态比较和展示钩子建立独立状态。公开的指针、map、slice、数组和循环引用可以复制；私有标量及 `time.Time` 的不可变资料保留。

非空私有可变引用，以及非空 channel、函数、unsafe pointer 返回 `Unsupported`。nil 引用合法。实体采用这些形状时，应明确其持久化和复制方式，再接入需要独立快照的用例。

## 审计与软删除

`AuditedEntity` 包含 ID、Version、创建 / 更新 / 删除时间及操作人。`SetUpdatedInfo` 设置更新人和时间，保留当前版本；`SoftDeleteBy` 要求非空 operator，`Restore` 清除删除信息。CRUD/Audited 仓储依据写入前版本统一计算下一版本，覆盖普通更新、软删、恢复、Transfer 与批量更新。物理 Delete/Purge 校验旧版本。普通实体显式使用 `WithoutVersioning` 时沿用退出版本保护的规则；审计实体保持版本保护。创建时审计实体初始版本归零，普通实体保留既有初始版本规则。

`IAuditStore` 定义审计记录存取。包含已删读取与已删除列表分别由 `IRestoreRepository`、`IDeletedQueryRepository` 表达。事务、operator 注入和审计持久化由 [app/audited](../app/README.md#审计型-crud)及具体仓储负责。

`AuditRecord.Dimensions` 保存全部维度的不可变标量快照，`OperationID` 关联一次逻辑操作。审计读取通过同一解释器对历史事实求值，缺键明确失败，实体物理删除后仍可查询。迁移两侧各保存自己的前后像快照。

## 事件溯源

`IDomainEvent` 只要求稳定的 `EventType()`。`EventSourcedAggregate` 管理版本与未提交事件，`ApplyAndRecord` 应用并记录事件，仓储以 `GetExpectedVersion()` 作为保存基线。

组合根显式创建 `MetadataRegistry`，可通过 `RegisterSet`、`Aggregate`、`RegisterModuleAggregates` 预热；`InitAggregate` / `New` 构造时也可按该 registry 确保 metadata 已就绪。

反射事件路由要求处理方法导出、事件参数为指针类型，同一 Go 事件类型只能映射一个处理方法。未匹配 handler 时返回错误，不能静默推进聚合版本。

领域侧存储端口为 `IDomainEventStore`，默认持久化适配和命令编排在 `app/eventsourced`。示例见[事件溯源速查](../docs/reference/ddd-eventsourcing-quick-reference.md)，存储事件的版本处理见[事件 Schema 与回放](../docs/reference/event-schema-evolution.md)。
