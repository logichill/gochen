# Gochen 框架架构

Gochen 通过接口和组合根组织领域建模、应用编排与基础设施。CRUD、审计型 CRUD、Event Sourcing 是可按业务选择的建模方式；采用消息总线或 CQRS 不要求所有写入都使用事件溯源。

## 仓库与依赖

| 仓库 / module | 内容 | 依赖边界 |
| --- | --- | --- |
| `gochen` | Core 契约、通用编排与内存实现 | 生产、测试、示例均只依赖标准库和本模块 |
| `gochen-runtime` | Host、DI、配置、net/http、REST、SQL 与安全装配 | 依赖 Core；第三方依赖受该仓库规则限制 |
| `gochen-contrib` | Gin、GORM、Redis 锁、观测驱动与迁移 CLI | 依赖 Core / Runtime 及对应第三方库 |

三个仓库各有独立 `go.mod`，开发态可由父目录 `go.work` 组合。仓库名、module path 与目录名一致；module path 不得使用标准库顶级包名。Core 不反向依赖 Runtime 或 Contrib。

生产代码的顶级包白名单与完整依赖矩阵由 [AGENTS.md](../../AGENTS.md#架构约定core-模块)统一维护。新增顶级包或跨能力域依赖边，须先修订该矩阵并说明理由。

## 能力分层

| 能力域 | 职责与关键边界 |
| --- | --- |
| `domain` | 实体、聚合、仓储端口；非测试代码只依赖标准库、`gochen/errors` 与自身子树 |
| `app` | 校验、事务、Hooks、查询、领域事件持久化与写操作协议；具体基础设施由组合根注入 |
| `auth` / `app/security` | 安全契约与应用层保护；只有 `app/security` 允许依赖 `auth`，基础应用模板不依赖它 |
| `eventing` | 事件模型、存储、Outbox、投影、订阅与监控；通过 `messaging` 进行投递 |
| `messaging` | 消息信封、总线、中间件、Transport 与命令执行；不依赖 `eventing` |
| `db` / `httpx` | 数据库、ORM 与 HTTP 契约；SQL、服务监听和 REST 注册在 Runtime |
| `process` / `policy` | Saga、Workflow、锁与任务监督；重试、限流、熔断属于 `policy` |
| 基础包 | `clock`、`codec`、`gen`、`validate` 只依赖标准库、`gochen/errors` 与自身子树，彼此协作使用最小接口 |

内存 Store、内存 Transport、缓存和 `testkit` 可用于无外部环境的测试，属于 Core。具体数据库连接、配置源和网络驱动由 Runtime / Contrib 承载。

## 领域与应用

`domain/crud.IRepository` 只要求 `Create`、`Update`、`Delete`、`Get`。查询、批量、物理删除等是可选能力，由 Application 或适配层在使用前探测。Repository 通过构造选项表达隔离与范围约束；包装窄仓储接口会丢失可选能力或假冒能力，因此禁止用仓储装饰器承载安全策略。

- CRUD：领域实体 → `app/crud.Application` → Runtime REST。
- 审计型 CRUD：增加 `domain/audited`、审计 Store 和事务能力，业务写与审计写同事务提交。
- 事件溯源：领域聚合 → `app/eventsourced` 仓储与命令服务 → EventStore；读侧可由投影维护。

`app/query` 定义适配器查询协议，业务领域查询可另设有明确业务含义的仓储方法。`app/operation` 表达一次写入的结果与可见状态；Saga / Workflow 表达多步骤过程。

模块细节见 [domain](../../domain/README.md)、[app](../../app/README.md)、[Operation](../../app/operation/README.md)和[事件溯源速查](../reference/ddd-eventsourcing-quick-reference.md)。

## 授权与上下文

身份认证在传输边界完成，tenant / operator / user / session / trace / request 通过 `contextx` 传播。Application 层是 HTTP、Worker、CLI 共同调用的授权边界，HTTP Action 中间件只负责提前拒绝。

Runtime `security.Profile.Build` 固定 `Action(Scoped(app))` 的装饰顺序。Scoped 将资源判定与数据范围转为受控写约束，仓储在最终写操作中原子合并隔离、范围、资源和版本条件。治理能力装饰 Authorizer，负责策略快照、决策审计与指标。具体契约见[分层授权](layered-authz.md)。

## 事件与消息链路

```text
命令执行 → 聚合应用事件 → 同事务写 EventStore 与 Outbox
                                      ↓
                                Publisher → EventBus
                                                ↓
                                      Projection → 读模型
```

`eventing.IEvent` 扩展 `messaging.IMessage`；消息以 `Type` 路由，`Kind` 标记消息类别。同步 Transport 的发布结果可反映处理器结果，异步 Transport 的发布成功表示接受消息，持久性取决于具体传输实现。

事件流由 `(aggregate_type, aggregate_id)` 定位，版本用于乐观并发控制。载荷消费统一经过 registry / upcast；启用 checkpoint 时，投影写入与 checkpoint 保存必须共享原子边界。Outbox 和重放均要求消费者幂等。细节集中在 [messaging](../../messaging/README.md)、[EventStore](../../eventing/store/README.md)、[Outbox](../../eventing/outbox/README.md)和[Projection](../../eventing/projection/README.md)。

## 装配与生命周期

组合根创建 DB、HTTP server、Transport、总线、ID 生成器、日志及配置，再通过构造函数或 option 注入。Runtime `quick` 提供常用装配入口，完整 Host / Repo API 支持定制；见 [Quick 装配](../guides/quick-assembly.md)。

| 组件语义 | 生命周期 |
| --- | --- |
| 后台服务 | `Start(ctx)` 非阻塞启动，`Stop(ctx)` 控制等待与收尾 |
| 进程主循环 | `Run(ctx)` 阻塞运行，`Shutdown(ctx)` 请求终止 |
| 资源句柄 | `Open` / `Close` 或 `Close` |

上述是命名约定；入口的具体阻塞行为以其契约为准，例如 `quick.Run` 调用的生命周期引擎 `Start(ctx)` 会等待运行结束。计时器的 `Stop()` 沿用标准库语义。

常规停止使用 `messaging.StopTransport`；需要保存或重投 pending 时，直接调用 `ITransportStopSnapshot.StopWithSnapshot` 并接收快照。超时快照可能包含尚未确认完成的消息，重投需按消息 ID 去重，详见 [messaging](../../messaging/README.md#停止与处理失败)。关闭共享依赖前，先等待依赖它的发布器、消费者和后台任务退出。

## 并发约定

| 原语 | 适用状态 | 约束 |
| --- | --- | --- |
| `atomic.Value` | 单槽配置或 recorder 热替换 | 动态类型固定，不存 nil，不拼装多字段不变量 |
| `sync.Mutex` | 生命周期迁移、多字段一致性、channel 关闭 | 锁内只做必要状态更新，避免持锁调用外部回调 |
| `sync.RWMutex` | 读多写少的 registry / cache / store | 不向调用方泄露内部可变 map / slice |

新增并发字段须说明保护对象和调用语义。同一投影的处理、追赶、恢复、重建串行；同一 Workflow 实例的进程内锁不能替代存储层的跨进程乐观锁。

## 验证与文档维护

各仓库独立执行 build、vet、test；Core 使用 `GOWORK=off` 验证零第三方依赖。默认测试不得依赖真实外部服务或开发机固定路径；Runtime 的内存 / 临时文件 SQLite 测试可进入默认测试集。真实数据库、消息队列、Docker 等环境测试使用 `integration` build tag，并说明资源与清理方式。

文档只描述现行契约、使用方式和限制。架构边界在本文说明，包级细节放模块 README，接入步骤放 guides，规格放 SPEC；同一内容用链接引用，避免重复维护 API 和 DDL。
