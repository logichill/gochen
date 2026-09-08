# Gochen 文档索引

本目录说明 Core 及其 Runtime、Contrib 配套能力。跨仓资料以仓库名和仓库内路径标注。

## 接入与架构

| 文档 | 内容 |
| --- | --- |
| [项目入口](../README.md) | 项目定位、模块边界、本地验证 |
| [下游接入指南](guides/downstream-guide.md) | 本地依赖、能力选择、组合根与接入约束 |
| [Quick 装配](guides/quick-assembly.md) | Repo / Application / REST / Host 工厂与列名约定 |
| [Contrib 适配器](guides/contrib-guide.md) | 第三方适配器职责与接入边界 |
| [框架架构](architecture/framework-design.md) | 能力分层、依赖方向、生命周期和并发约定 |
| [分层授权](architecture/layered-authz.md) | L0–L4、DataScope、写入约束与跨隔离读取 |
| [开发规范](../SPEC.md) / [仓库约定](../AGENTS.md) | 编码规则、顶级包与依赖矩阵 |

首次接入可按“项目入口 → 下游接入指南 → Quick 装配 → 示例”阅读。

## Core 模块

| 能力 | 文档 |
| --- | --- |
| 领域与应用 | [domain](../domain/README.md)、[app](../app/README.md)、[Operation](../app/operation/README.md) |
| HTTP 与 ORM 契约 | [httpx](../httpx/README.md)、[db/orm](../db/orm/README.md) |
| 事件驱动 | [eventing](../eventing/README.md)、[EventBus](../eventing/bus/README.md)、[EventStore](../eventing/store/README.md)、[Outbox](../eventing/outbox/README.md)、[Projection](../eventing/projection/README.md) |
| 消息 | [messaging](../messaging/README.md)、[Command](../messaging/command/README.md) |
| 过程与策略 | [process](../process/README.md)、[Saga](../process/saga/README.md)、[Workflow](../process/workflow/README.md)、[policy](../policy/README.md) |
| 错误与日志 | [errors](../errors/README.md)、[logging](../observe/logging/README.md) |

## 专题与示例

- [DDD / Event Sourcing / CQRS 速查](reference/ddd-eventsourcing-quick-reference.md)。
- [事件 Schema 与回放](reference/event-schema-evolution.md)。
- [Saga 生命周期事件](reference/eventing-saga-events.md)。
- [数据库 Schema 与迁移](guides/db-schema-migration-guide.md)。
- [Core 示例](../examples/README.md)。
- `gochen-runtime` 仓库 `host/README.md`、`api/rest/README.md`、`http/README.md`、`examples/README.md`。
