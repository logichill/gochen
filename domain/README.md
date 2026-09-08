# domain：领域建模契约

`domain` 是业务领域可复用的共享内核，提供实体、聚合、仓储端口与领域约束。非测试代码只依赖标准库、`gochen/errors` 和自身子树。

## 建模方式

| 子包 | 内容 | 场景 |
| --- | --- | --- |
| `domain/crud` | `Entity`、`TenantEntity`、仓储与可选查询 / 批量接口 | 一般管理数据 |
| `domain/audited` | 审计字段、软删除、恢复和审计存储端口 | 需要审计轨迹的管理数据 |
| `domain/eventsourced` | 聚合基类、metadata、领域事件存储与仓储端口 | 通过事件流重建业务状态 |

## 实体与仓储

- `IEntity[ID]` 要求 `GetID()` 和 `GetVersion()`；`ISettableID` 提供可选 ID 回填能力。
- `IValidatable.Validate()` 表达不依赖外部资源的领域不变量。需要数据库或上下文的校验通过应用层注入。
- `ITimestamps` 与 `ISoftDeletable` 是独立能力；带 operator 的软删除由 `domain/audited` 补充。
- `domain/crud.IRepository` 只有 `Create`、`Update`、`Delete`、`Get`。`IQueryRepository`、`IBatchOperations`、`IPurgeRepository` 等按需实现。

仓储错误通过 `gochen/errors` 表达；基础仓储的 Delete 可采用幂等或严格未命中语义，具体实现须明确。领域查询优先使用有业务含义的方法；`app/query` 是适配器查询协议，不属于领域层依赖。

`TenantEntity` 只声明租户字段与访问方法，不负责开启隔离。隔离在 Runtime Repo 构造期声明，详见[分层授权](../docs/architecture/layered-authz.md)。

## 审计与软删除

`AuditedEntity` 包含 ID、Version、创建 / 更新 / 删除时间及操作人。`SetUpdatedInfo` 推进版本；`SoftDeleteBy` 要求非空 operator，`Restore` 清除删除信息。

`IAuditStore` 定义审计记录存取。包含已删读取与已删除列表分别由 `IRestoreRepository`、`IDeletedQueryRepository` 表达。事务、operator 注入和审计持久化由 [app/audited](../app/README.md#审计型-crud)及具体仓储负责。

## 事件溯源

`IDomainEvent` 只要求稳定的 `EventType()`。`EventSourcedAggregate` 管理版本与未提交事件，`ApplyAndRecord` 应用并记录事件，仓储以 `GetExpectedVersion()` 作为保存基线。

组合根显式创建 `MetadataRegistry`，可通过 `RegisterSet`、`Aggregate`、`RegisterModuleAggregates` 预热；`InitAggregate` / `New` 构造时也可按该 registry 确保 metadata 已就绪。

反射事件路由要求处理方法导出、事件参数为指针类型，同一 Go 事件类型只能映射一个处理方法。未匹配 handler 时返回错误，不能静默推进聚合版本。

领域侧存储端口为 `IDomainEventStore`，默认持久化适配和命令编排在 `app/eventsourced`。示例见[事件溯源速查](../docs/reference/ddd-eventsourcing-quick-reference.md)，存储事件的版本处理见[事件 Schema 与回放](../docs/reference/event-schema-evolution.md)。
