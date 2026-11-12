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
| `auth` | 授权端口（`IAuthorizer`）、动作检查、范围规约与治理装饰器；业务 Application 通过 Options 接入 |
| `eventing` | 事件模型、存储、Outbox、投影、订阅与监控；通过 `messaging` 进行投递 |
| `messaging` | 消息信封、总线、中间件、Transport 与命令执行；不依赖 `eventing` |
| `db` / `httpx` | 数据库、ORM 与 HTTP 契约；SQL 方言契约保留 Core；具体方言渲染、SQL 扫描、服务监听和 REST 注册在 Runtime |
| `process` / `policy` | Saga、Workflow、锁与任务监督；重试、限流、熔断属于 `policy` |
| 基础包 | `clock`、`codec`、`gen`、`validate` 只依赖标准库、`gochen/errors` 与自身子树，彼此协作使用最小接口 |

内存 Store、内存 Transport、缓存和 `testkit` 可用于无外部环境的测试，属于 Core。具体数据库连接、配置源和网络驱动由 Runtime / Contrib 承载。

## 领域与应用

`domain/crud.IRepository` 要求 `Create`、`Update`、`Delete`、`Get`。Application 按用例要求查询、事务等附加仓储能力；审计应用进一步要求恢复、已删除查询和永久删除。Repository 在构造期声明隔离与资源契约，授权条件由 Application 显式传入每次调用。

- CRUD：领域实体 → `app/crud.Application` → Runtime REST。
- 审计型 CRUD：增加 `domain/audited`、审计 Store 和事务能力，业务写与审计写同事务提交。
- 事件溯源：领域聚合 → `app/eventsourced` 仓储与命令服务 → EventStore；读侧可由投影维护。

`app/query` 定义适配器查询协议，业务领域查询可另设有明确业务含义的仓储方法。`app/operation` 表达一次写入的结果与可见状态；Saga / Workflow 表达多步骤过程。

模块细节见 [domain](../../domain/README.md)、[app](../../app/README.md)、[Operation](../../app/operation/README.md)和[事件溯源速查](../reference/ddd-eventsourcing-quick-reference.md)。

## 授权与上下文

身份认证在传输边界完成，授权主体、当前范围与系统标记统一读取根 `auth.Principal`；Runtime 权限资料绑定到该身份及当前租户。tenant / operator / user / session / trace / request 通过 `contextx` 传播。Application 层是 HTTP、Worker、CLI 共同调用的授权边界，HTTP Action 中间件只负责提前拒绝。

普通用例通过 `auth.IAuthorizer` 在入口授权一次，Transfer 在事务外分别授权转出和接收。返回的 `spec.Spec` 随仓储调用执行，SQL 写入同时约束隔离、授权与版本，并校验目标状态。L0–L4 的职责见 [分层授权](layered-authz.md)，字段保护和 Transfer 见 [资源契约](resource-contracts.md)。

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

事件存储按消费者拆分能力：领域适配需要追加、聚合版本与聚合分页；历史分页只读聚合版本与分页；投影只扫描全局流。`IEventStreamStore` 是完整后端与缓存的组合契约，不作为所有消费者的必需接口。原子批量能力只要求 `AppendEventsBatch`，不得以循环单项追加模拟。SQL Outbox 的事务追加端口由 Runtime `eventing/outbox/sqlstore` 定义，Core EventStore 不接收数据库句柄。

后端通过 Core `eventing/store/contracttest` 共享行为测试验证版本冲突、重试、分页与回滚语义；内存实现和 Runtime 两种 SQL 游标模式均执行同一套契约，驱动特有行为由各实现补充验证。

## 装配与生命周期

组合根创建 DB、HTTP server、Transport、总线、ID 生成器、日志及配置，再通过构造函数或 option 注入。Runtime `quick` 提供常用装配入口，完整 Host / Repo API 支持定制；见 [Quick 装配](../guides/quick-assembly.md)。

| 组件语义 | 生命周期 |
| --- | --- |
| 后台服务 | `Start(ctx)` 非阻塞启动，`Stop(ctx)` 控制等待与收尾 |
| 进程主循环 | `Run(ctx)` 阻塞运行，`Shutdown(ctx)` 请求终止 |
| 资源句柄 | `Open` / `Close` 或 `Close` |

上述是命名约定；入口的具体阻塞行为以其契约为准，例如 `quick.New(...).Run(ctx)` 调用的生命周期引擎 `Start(ctx)` 会等待运行结束。计时器的 `Stop()` 沿用标准库语义。

常规停止使用 `messaging.StopTransport`；需要保存或重投 pending 时，直接调用 `ITransportStopSnapshot.StopWithSnapshot` 并接收快照。超时快照可能包含尚未确认完成的消息，重投需按消息 ID 去重，详见 [messaging](../../messaging/README.md#停止与处理失败)。关闭共享依赖前，先等待依赖它的发布器、消费者和后台任务退出。

## 并发约定

| 原语 | 适用状态 | 约束 |
| --- | --- | --- |
| `atomic.Value` | 单槽配置或 recorder 热替换 | 动态类型固定，不存 nil，不拼装多字段不变量 |
| `sync.Mutex` | 生命周期迁移、多字段一致性、channel 关闭 | 锁内只做必要状态更新，避免持锁调用外部回调 |
| `sync.RWMutex` | 读多写少的 registry / cache / store | 不向调用方泄露内部可变 map / slice |

新增并发字段须说明保护对象和调用语义。同一投影的处理、追赶、恢复、重建串行；同一 Workflow 实例的进程内锁不能替代存储层的跨进程乐观锁。

## 验证与文档维护

各仓库独立执行 build、vet、test；Core 使用 `GOWORK=off` 验证零第三方依赖。默认测试使用内存或临时 SQLite，不依赖外部服务或开发机固定路径。真实数据库、消息队列、设备等测试必须显式启用（`integration` build tag 或测试注明的环境变量），并说明资源和清理方式。

文档只描述现行契约、使用方式和限制。架构边界在本文说明，包级细节放模块 README，接入步骤放 guides，规格放 SPEC；同一内容用链接引用，避免重复维护 API 和 DDL。

## 可选适配与提交结果

事件溯源核心模板及历史读模型位于 `app/eventsourced`；默认存储/快照适配位于 `app/eventsourced/eventstore`，订阅/投影辅助位于 `app/eventsourced/subscriber`。组合根按需装配，核心模板直接消费 `store.IAggregateEventReader`。

Operation SSE 位于 Runtime `http/operation`；监控端点策略及公开响应整形位于 Runtime `eventing/monitoring`。Core 监控包保留指标、健康与快照模型。方言接口和值类型仍在 Core `db/dialect`，标准方言与 SQL 词法实现分别位于 Runtime `db/dialect` 和 `db/sqlscan`。Outbox 表名与 SQL 映射由 Runtime SQL 仓储负责，Core DTO 不携带 GORM 标签。

`contextx.AfterCommitError` 统一表示主操作成功、后续步骤失败。ES 仓储据此更新提交基线，命令重试排除该类错误，Operation 使用 degraded 表达 handler 的提交后失败。若业务成功但 Operation 结果记录失败，则保留幂等预占并返回原结果供核对和补存。

`app/crud.IApplication` 提供读写、查询、批量执行与动作描述。配置、授权器、校验器和 Hooks 在构造时确定；审计应用增加生命周期能力。Runtime REST 根据 Descriptor 注册路由，在装配期校验动作所需能力。应用与 HTTP 分别配置业务和传输配额，HTTP 上限受应用上限约束。用例与装配细节见 [应用模板](../../app/README.md)及 [Quick 装配](../guides/quick-assembly.md)。
