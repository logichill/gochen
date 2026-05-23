# Gochen

面向企业业务系统的开放式 DDD 基础框架。

Gochen 为业务仓库提供一套可渐进采用的企业级架构骨架，覆盖分层建模、应用服务模板、HTTP 适配、数据访问、事件驱动、消息通信、运行时治理与模块化装配等通用能力，帮助团队把精力集中在业务规则，而不是在每个项目里重复搭建基础设施。

[![Go Version](https://img.shields.io/badge/Go-1.26%2B-blue)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-yellow)](./LICENSE)

## 项目定位

Gochen 不是“只能按一种方式工作的重型框架”，也不是“零散工具库集合”。它的定位更接近：

- 企业级 DDD / CQRS / Event Sourcing 公共骨架
- 面向组合根装配的开放式框架
- 可在 CRUD、审计型业务、事件驱动业务之间渐进演进的基础设施平台

这里的“开放式”指的是：

- 接口优先，依赖抽象而不是绑定具体实现
- 组合根显式装配，不靠隐式魔法生成依赖
- 不绑定具体 Web 框架、ORM 或消息中间件
- 已有能力可直接复用，特殊场景也允许在标准边界内扩展或替换

## Gochen 解决什么问题

企业项目在走到一定规模后，往往会在多个仓库中反复实现同一类基础能力：

- 模块生命周期与启动装配
- CRUD / 审计 / 事件溯源应用模板
- HTTP 路由注册与统一响应约定
- Query / Filter / Paging 协议
- 事件存储、投影、Outbox、消息投递
- 错误模型、上下文语义、日志与校验

Gochen 的目标就是把这些跨项目的共性能力沉淀为稳定框架能力，让下游项目共享同一套架构语言、治理约束与最佳实践，而不是各自再造一轮。

## 核心设计原则

- 接口优先：基础能力先定义接口，再提供默认实现。
- 组合根装配：依赖由业务项目显式创建和注入，框架不偷偷决定外部实现。
- 分层清晰：`domain` 保持纯净，`app` 负责用例编排，`eventing` / `messaging` / `db` / `httpx` 承担基础设施能力。
- 默认安全：输入校验、SQL 边界、HTTP 语义、上下文约束遵循 fail-fast。
- 渐进演进：支持从 CRUD 到 audited，再到 event sourced / CQRS 的逐步升级。
- 优先复用：框架已覆盖的共性能力，下游项目应优先采用标准能力，而不是重复实现。

## 能力版图

| 能力域             | 主要模块                                                                           | 适用问题                                                      |
| ------------------ | ---------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| 领域建模与应用模板 | `domain`、`app`                                                                    | 实体、聚合、仓储端口、CRUD / 审计 / 事件溯源应用服务          |
| 模块装配与生命周期 | `host`、`di`                                                                       | 进程生命周期、模块初始化、依赖注入、组合根治理                |
| HTTP 与 API        | `httpx`、`api/rest`                                                               | 框架无关 HTTP 抽象、REST CRUD 路由注册、统一响应约定          |
| 数据访问           | `db`                                                                               | Query DSL、ORM 抽象、SQL Builder、方言、安全边界              |
| 事件驱动           | `eventing`                                                                         | Event Store、Projection、Outbox、Subscription、Monitoring     |
| 消息通信           | `messaging`                                                                        | MessageBus、CommandBus、Transport、中间件、DLQ                |
| 过程与治理         | `app/operation`、`process`、`policy`、`task`                                        | Operation、Saga、Workflow、重试、限流、熔断、任务监督         |
| 通用运行时能力     | `errors`、`auth`、`domain/access`、`auth/http`、`auth/sqlstore`、`contextx`、`logging`、`validate`、`clock`、`config`、`codec`、`ident` | 错误语义、身份与授权上下文、链路传播、日志、校验、时间、配置、编解码、ID 策略 |

完整能力边界、下游应该优先采用什么、哪些能力不应重复实现，请直接看
[docs/guides/downstream-guide.md](docs/guides/downstream-guide.md)。

## 仓库结构速览

如果你想先建立“这个仓库大致由哪些部分组成”的全景感，可以先按下面的分组理解：

- `domain` / `app`：领域建模与应用服务模板
- `host` / `di`：生命周期、模块装配与依赖注入
- `httpx` / `api/rest`：HTTP 抽象与 REST 交付层
- `db`：Query、ORM、SQL Builder、安全边界与方言适配
- `eventing` / `messaging`：事件驱动、消息投递、Outbox、Projection、CommandBus
- `app/operation` / `process` / `policy` / `task`：写操作协议、过程运行时、控制策略与后台任务监督
- `errors` / `auth` / `domain/access` / `auth/http` / `auth/sqlstore` / `contextx` / `logging` / `validate` / `clock` / `config` / `codec` / `ident`：通用运行时能力
- `examples`：可运行示例
- `docs`：文档门户、接入指南与架构设计

更完整的能力索引与模块职责，见 [docs/guides/downstream-guide.md](docs/guides/downstream-guide.md)。

## 使用前先知道的关键约定

下面这些约定会直接影响下游项目的接入方式，建议在开始使用前先过一遍：

- `ident`：统一通过 `IGenerator` 及其实现子包管理 ID 生成策略，不要在下游项目再平行定义一套 ID 生成入口。
- `auth`：tenant / operator / user / session / principal / authorization、策略快照、评估上下文、授权日志与指标等安全运行时语义统一从这里进入；`domain/access` 承载 repo/app 层消费的 DataScope/WriteConstraint contract，auth 提供决策投影；HTTP 与 SQL 持久化桥接分别位于 `auth/http`、`auth/sqlstore`。
- `contextx`：trace / request / tx scope / 元数据传播等链路语义继续在这里定义；桥接层只负责传播，不再各自长出平行语义。
- `config.Provider`：`GetString` 仅接受 `string/[]byte`；`Has` 把 `nil` 视为缺失；`GetStringSlice` 会对逗号分隔字符串做 `TrimSpace` 并过滤空元素。
- `codec`：根包只定义统一 `ICodec[T, Raw]` 接口；`codec/idcodec` 负责 ID/raw 值编解码，`codec/jsoncodec` 负责 JSON 文本编解码。

### Auth 包路径约定

下游默认只需要 `gochen/auth` 这一个安全运行时主入口；写入约束、repo data scope 位于 `gochen/domain/access`，auth 决策到 repo 约束的投影由 `gochen/auth` 提供。HTTP 与 SQL 持久化桥接分别位于 `gochen/auth/http`、`gochen/auth/sqlstore`；CRUD tenant 策略由组合根通过 `gochen/app/crud` 显式注入。

## 推荐采用路径

Gochen 推荐下游项目按业务复杂度渐进采用：

| 阶段          | 适用场景                         | 推荐模块                                                                  |
| ------------- | -------------------------------- | ------------------------------------------------------------------------- |
| CRUD          | 配置、字典、简单后台管理域       | `domain/crud` + `app/crud` + `api/rest`                                |
| Audited CRUD  | 需要审计、软删、恢复的管理域     | `domain/audited` + `app/audited` + `api/rest`                          |
| Event Sourced | 关键交易域、强审计域、事件驱动域 | `domain/eventsourced` + `app/eventsourced` + `eventing/*` + `messaging/*` |

如果你在业务仓库里已经开始手写这些基础骨架，通常说明应该先回头检查 Gochen 是否已经提供了更标准的实现方式。

## 下游项目应该如何使用 Gochen

Gochen 的目标不是让下游“依赖所有模块”，而是要求下游对已覆盖的通用能力优先复用框架标准能力。

对于下游项目，建议遵循三条基本原则：

- Gochen 已覆盖的共性基础能力，优先复用，不重复实现。
- 业务仓库负责业务规则与业务装配，不负责重造框架级抽象。
- 需要偏离标准能力时，应明确说明为什么 Gochen 现有能力不适用。

接入原则、能力边界、治理约束与评审清单见 [docs/guides/downstream-guide.md](docs/guides/downstream-guide.md)。

## 快速开始

先运行仓库示例与测试，建立整体认知：

```bash
go test ./... -count=1
go run ./examples/domain/crud
go run ./examples/domain/audited
go run ./examples/domain/eventsourced
```

更多示例入口见 [examples/README.md](examples/README.md)。

## 阅读路径

如果你是第一次接触 gochen，建议按下面顺序阅读：

1. [README.md](README.md)：理解项目定位、价值与采用路径。
2. [docs/README.md](docs/README.md)：进入完整文档门户。
3. [docs/guides/downstream-guide.md](docs/guides/downstream-guide.md)：能力边界、下游接入原则与治理约束。
4. [docs/architecture/framework-design.md](docs/architecture/framework-design.md)：深入理解整体架构设计与模块边界。
5. [examples/README.md](examples/README.md)：结合示例理解装配方式和演进路径。

## 模块导航

按关注点继续深入：

- 应用层模板：[app/README.md](app/README.md)
- 领域建模：[domain/README.md](domain/README.md)
- 模块生命周期：[host/README.md](host/README.md)
- HTTP 抽象：[httpx/README.md](httpx/README.md)
- REST 注册：[api/rest/README.md](api/rest/README.md)
- 事件基础设施：[eventing/README.md](eventing/README.md)
- 消息内核：[messaging/README.md](messaging/README.md)
- 写操作协议：[app/operation/README.md](app/operation/README.md)
- 过程运行时：[process/README.md](process/README.md)
- 控制策略：[policy/README.md](policy/README.md)

## 文档入口

- 文档门户：[docs/README.md](docs/README.md)
- 下游接入指南：[docs/guides/downstream-guide.md](docs/guides/downstream-guide.md)
- 框架设计：[docs/architecture/framework-design.md](docs/architecture/framework-design.md)
- DDD / Event Sourcing 速查：[docs/reference/ddd-eventsourcing-quick-reference.md](docs/reference/ddd-eventsourcing-quick-reference.md)
- DB Schema 迁移：[docs/guides/db-schema-migration-guide.md](docs/guides/db-schema-migration-guide.md)
- 示例索引：[examples/README.md](examples/README.md)

## 面向贡献者

Gochen 自身的开发规范仍以 [SPEC.md](SPEC.md) 为准；它约束的是框架仓库内部如何设计和实现，而不是下游项目的接入方式。

在修改代码或文档前，建议先阅读：

- [SPEC.md](SPEC.md)
- [docs/architecture/framework-design.md](docs/architecture/framework-design.md)
- [docs/README.md](docs/README.md)

## 许可证

MIT License，见 [LICENSE](LICENSE)。
