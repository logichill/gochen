# examples/：可运行示例索引

该目录保留“先跑通主路径，再按能力查参考”的示例结构。新用户优先看第 1 节，确认框架推荐的装配方式；第 2 节用于查专项能力。

## 1. 推荐主路径

```bash
# 1) 标准 CRUD REST：domain/crud + app/crud + api/rest
go run ./examples/domain/crud

# 2) Auth 授权执行：Principal、Authorizer、AuthzDecision 与资源解析
go run ./examples/auth/authorization

# 3) Event Sourcing：聚合、事件存储、投影与 Outbox 的基本组合
go run ./examples/domain/eventsourced

# 4) Host module boot：host.Module、host.Run、模块路由与 auth 目录注册
go run ./examples/host/module_boot

# 5) SQL Outbox：事件存储与 Outbox 持久化链路
go run ./examples/infra/outbox/sql
```

## 2. 参考示例

### 2.1 Domain / API

| 目录                                    | 主题           | 适用场景                                      |
| --------------------------------------- | -------------- | --------------------------------------------- |
| `examples/domain/audited`               | audited CRUD   | 需要审计、软删、恢复与 operator 注入          |
| `examples/domain/query/inferred`        | 查询协议推导   | 需要从实体 `query` tag 自动推导 filter/sort   |
| `examples/domain/query/filter`          | 业务 filter    | 需要把统一查询协议绑定到业务自定义过滤对象    |
| `examples/domain/eventsourced_stringid` | string/UUID ID | 非 `int64` 聚合 ID 的事件存储接口实现         |

### 2.2 Host / Auth

| 目录                            | 主题     | 适用场景                                               |
| ------------------------------- | -------- | ------------------------------------------------------ |
| `examples/auth/authorization`    | 授权执行 | 需要学习 `Principal`、`Authorize`、`AuthzDecision`      |
| `examples/host/module_boot`      | 模块启动 | 需要学习 `host.Module(...)` 与 `host.Run(...)`          |

### 2.3 Eventing / Infra

| 目录                                       | 主题                         | 适用场景                                      |
| ------------------------------------------ | ---------------------------- | --------------------------------------------- |
| `examples/infra/outbox/es_mock`            | Outbox mock                  | 只想看 Outbox 接口与发布骨架                  |
| `examples/infra/outbox/sql`                | SQL Outbox                   | 需要 SQLite + SQLEventStore + SQL Outbox Repo |
| `examples/infra/projection/basic`          | Projection basic             | 需要理解投影管理器的启动与追赶                |
| `examples/infra/projection/idempotent`     | Projection 幂等              | 需要基于 `event.ID` 做幂等写入                |
| `examples/infra/projection/sql_checkpoint` | Projection SQL checkpoint    | 需要持久化 checkpoint 与恢复                  |
| `examples/infra/snapshot/basic`            | Snapshot                     | 需要降低聚合重建成本                          |
| `examples/infra/upcaster/basic`            | Upcaster                     | 需要演示历史事件 schema 升级                  |
| `examples/infra/sqlbuilder/basic`          | SQL Builder                  | 需要看 `db/sql/sqlbuilder` 的参数化构造用法   |

### 2.4 Messaging / Process / Context

| 目录                                 | 主题            | 适用场景                         |
| ------------------------------------ | --------------- | -------------------------------- |
| `examples/messaging/command_service` | command/service | 需要命令处理与事件发布模板       |
| `examples/process/saga/basic`        | Saga            | 需要跨步骤编排与补偿的最小骨架   |
| `examples/contextx/tracing/basic`    | tracing         | 需要 trace_id 在命令/事件链路贯通 |

## 3. 说明

- `examples/internal/mocks` 只服务示例和测试，不作为生产适配层使用。
- 进一步阅读：`README.md`、`docs/README.md`、`docs/guides/downstream-guide.md`、`docs/architecture/framework-design.md`、`docs/reference/ddd-eventsourcing-quick-reference.md`。
