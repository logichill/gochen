# domain 包

## 定位

`domain` 是 **DDD 共享内核 / 建模工具箱**，而非具体业务的领域层。

它为业务领域层提供可复用的基础抽象：

- 实体接口（`IEntity`）与领域事件接口（`IDomainEvent`）
- 横切能力接口（`ITimestamps`、`IValidatable`、`ISettableID`）
- 三种建模风格的仓储 ports 与实体基类：
  - `domain/crud` — 简单 CRUD
  - `domain/audited` — 审计 + 软删除
  - `domain/eventsourced` — 事件溯源

## 分层边界

```
┌─────────────────────────────────────────────────┐
│  业务领域层（your-app/domain/...）              │  ← 具体业务实体与聚合
├─────────────────────────────────────────────────┤
│  共享内核（gochen/domain）                      │  ← 本包
├─────────────────────────────────────────────────┤
│  应用层（gochen/app）                           │  ← 用例编排与 API 构建
├─────────────────────────────────────────────────┤
│  基础设施层（gochen/db, gochen/cache, eventing, http, ...） │  ← 技术实现
└─────────────────────────────────────────────────┘
```

**依赖方向**：domain 不依赖任何外层包（仅依赖标准库与 `gochen/errors`）。

## 核心约定

### 错误策略

domain 内部使用 `gochen/errors` 返回结构化错误，这是共享内核的策略选择：

- 仓储 ports 的错误契约（NotFound / Conflict / Concurrency）通过 `gochen/errors` 错误码表达
- 调用方可通过 `errors.Is(err, errors.NotFound)` 等方式进行处理
- 若业务领域层希望与框架解耦，可在外层做 Normalize/映射

### 查询 DSL（Filters）

`gochen/db/query.QueryOptions.Filters` 是 **适配器查询协议**，用于 app ↔ 基础设施层的通信：

- 采用结构化表达（`[]Filter`：Field + Op + Value/Values），避免魔法字符串
- 字段白名单/注入安全由仓储实现负责（如 `db/orm/repo` 的 `isAllowedField` 校验）
- 业务领域层不应长期绑定此 DSL；若需领域语义查询，建议在仓储接口定义领域方法（如 `FindByEmail`）

### 序列化标签

基类（如 `Timestamps`、`AuditedEntity`）携带 `json` tag，这是共享内核的实用主义选择：

- 便于快速原型与 DTO 复用
- 若业务需要纯领域模型，建议将 json tag 移到外层 DTO/adapter

### 事件溯源约定

`domain/eventsourced` 的反射路由约束（详见包级注释）：

- 事件处理方法必须是导出方法
- 事件参数必须是指针类型（如 `*Deposited`）
- 同一 Go 类型只能有一个处理方法
- 推荐优先在启动期通过显式 `MetadataRegistry.RegisterSet(...)`、`Aggregate(...)` + `RegisterModuleAggregates(registry, ...)` 或 `host.Module(...).Aggregate(...)` 完成 metadata 预热；若调用方未显式预热，构造路径中的 `InitAggregate(registry, ...)` / `New(registry, ...)` 也会基于显式 registry 自动 ensure metadata
- `ScanMetadata(...) + BindMetadata(...)` 仅保留给底层或特殊场景；默认业务路径不再在运行时扫描 metadata

回放与演进（建议）：

- `app/eventsourced.DomainEventStore.RestoreAggregate` 在回放阶段对自动路由 handler 采用 fail-fast 语义：事件未命中 handler 时直接报错，避免状态静默漂移；
- 事件演进建议采用 reader-first/writer-second + schema_version + upcaster（参考 `docs/ddd-eventsourcing-quick-reference.md` 的“事件演进与滚动升级”章节）。

## 子包概览

| 子包 | 职责 |
|---|---|
| `domain/crud` | CRUD 实体基类 + 仓储 ports |
| `domain/audited` | 审计/软删能力接口 + 实体基类 + 仓储 ports |
| `domain/eventsourced` | 事件溯源聚合根基类 + 仓储 ports + 反射元数据 |

## 演进路径

| 阶段 | 适用场景 | 模块 |
|---|---|---|
| CRUD | 配置/字典/简单管理 | `domain/crud` |
| Audited CRUD | 需要审计/软删/恢复 | `domain/audited` |
| Event Sourced | 关键交易/强审计/事件驱动 | `domain/eventsourced` |
