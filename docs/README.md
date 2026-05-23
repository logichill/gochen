# gochen 文档门户

本目录承载 gochen 的架构设计、接入指南与速查资料。

## 推荐阅读路径

### 第一次接触 gochen

1. [../README.md](../README.md)
2. [guides/downstream-guide.md](guides/downstream-guide.md)
3. [architecture/framework-design.md](architecture/framework-design.md)
4. [../examples/README.md](../examples/README.md)

### 作为下游项目接入 gochen

1. [guides/downstream-guide.md](guides/downstream-guide.md)
2. [../app/README.md](../app/README.md)
3. [../host/README.md](../host/README.md)
4. [../api/rest/README.md](../api/rest/README.md)
5. [../eventing/README.md](../eventing/README.md)

### 作为 gochen 贡献者或维护者

1. [../SPEC.md](../SPEC.md)
2. [architecture/framework-design.md](architecture/framework-design.md)

## 文档分层

### `architecture/`

面向"想理解为什么这样设计"的读者，关注整体架构、模块边界与设计原则。

- [architecture/framework-design.md](architecture/framework-design.md)：框架整体架构、分层、模块边界与关键设计约束
- [architecture/rbac-to-layered-authz.md](architecture/rbac-to-layered-authz.md)：从 RBAC 一步步走到分层授权的概念演化与设计思路
- [architecture/authz-boundary-redesign.md](architecture/authz-boundary-redesign.md)：授权域、资源归属与业务数据可见性的解耦改造方案
- [architecture/review-remediation-plan.md](architecture/review-remediation-plan.md)：架构评审后续整改计划与验收标准

先读 [architecture/framework-design.md](architecture/framework-design.md) 建立全局认知；想理解授权模型为什么长这样，读 [rbac-to-layered-authz.md](architecture/rbac-to-layered-authz.md)。

### `guides/`

面向"要在项目里落地 gochen"的读者，关注接入方式、迁移路径与使用建议。

- [guides/downstream-guide.md](guides/downstream-guide.md)：下游项目接入原则、治理约束与评审清单
- [guides/db-schema-migration-guide.md](guides/db-schema-migration-guide.md)：事件存储、Outbox、Projection 相关表结构迁移指南

### `reference/`

面向"快速查速查和术语"的读者。

- [reference/ddd-eventsourcing-quick-reference.md](reference/ddd-eventsourcing-quick-reference.md)：DDD / Event Sourcing / CQRS 速查
- [reference/event-schema-evolution.md](reference/event-schema-evolution.md)：事件 schema 演进与滚动升级纪律
- [reference/eventing-saga-events.md](reference/eventing-saga-events.md)：Saga 生命周期事件说明

## 按主题找文档

| 你关心的问题                         | 建议阅读                                                                                         |
| ------------------------------------ | ------------------------------------------------------------------------------------------------ |
| gochen 的项目定位与价值              | [../README.md](../README.md)                                                                     |
| 下游项目怎么接入、怎么避免重复造轮子 | [guides/downstream-guide.md](guides/downstream-guide.md)                                         |
| gochen 到底提供了哪些能力            | [guides/downstream-guide.md](guides/downstream-guide.md)                                         |
| 各模块的边界与设计理由               | [architecture/framework-design.md](architecture/framework-design.md)                             |
| 分层授权模型怎么从 RBAC 演化来       | [architecture/rbac-to-layered-authz.md](architecture/rbac-to-layered-authz.md)                   |
| DDD / Event Sourcing 快速上手        | [reference/ddd-eventsourcing-quick-reference.md](reference/ddd-eventsourcing-quick-reference.md) |
| 数据表迁移与 schema 设计             | [guides/db-schema-migration-guide.md](guides/db-schema-migration-guide.md)                       |
| 可运行示例从哪里看                   | [../examples/README.md](../examples/README.md)                                                   |
