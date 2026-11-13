# AGENTS 准则（Gochen Core）

本仓库为 Gochen Core（module `gochen`），Gochen 框架的契约层——领域、应用、认证、事件、消息、策略、流程与 HTTP 抽象；规范见 `SPEC.md`。

- `../gochen-runtime`（Runtime，module `gochen-runtime`）：Core 的物理驱动层，局部约定见该仓库 `AGENTS.md`
- `examples/`：仅依赖 Core 的可运行示例；需要 Runtime 的示例位于 `gochen-runtime` 仓库 `examples/`

## 通用准则

- Go 项目：遵守 Go 编码规范与 `SPEC.md`。
- 重构不保留兼容层代码，保持项目的干净清爽。
- 执行代码创建/修改后要执行 go fmt
- 默认验证：**各仓库内**执行 `go build ./... && go vet ./... && go test -count=1 ./...`；全量入口见 `Makefile`。禁止退化为 workspace 级一把梭，否则 Core 零依赖不变量会被掩盖。

## 命名铁律

- **repo 名 = module path = 目录名**，三者必须一致（当前：`gochen` / `gochen-runtime`）。
- module path **禁止使用 Go 标准库顶级包名**，避免标准库与本地模块导入冲突。新增 module 前必须核对 `go list std | rg -x '<候选名>'` 为空。
- 生态内 module 一律 `gochen` 或 `gochen-*` 前缀，平级 dotless。

## 模块边界与依赖

- 依赖硬规则：只允许 `gochen-runtime → gochen` 单向依赖，禁止任何反向引用；Core 任何文件（含测试与示例）禁止 import `gochen-runtime`；SQL、配置加载、net/http server、Host/DI、api/rest 属于 Runtime。
- Core 生产与测试代码均实现零外部第三方依赖；`go.mod` 无任何直接或间接第三方 require 项，且不应存在 `go.sum`。门禁校验：在本仓库执行 `GOWORK=off go list -m all` 输出必须只有 `gochen` 自身。
- Runtime 第三方依赖仅限物理驱动所需（sqlite/yaml/uuid/testify）。
- 开发态由 workspace 级 `go.work`（位于两仓库的父目录）组合；Runtime 发布态必须改为真实 Core 版本，不依赖本地 replace。

## 下游项目

有且仅有以下项目为本项目的下游（`gochen-runtime`、`gochen-contrib` 与本仓库合称核心三包）：

- ../gochen-runtime
- ../gochen-contrib
- ../gochen-iam
- ../gochen-llm
- ../gochen-workflow
- ../alife
- ../ems
- ../erp

`../gochen-workflow` 不是本项目下游测试目标；仅当下游本地 replace/workspace 需要时，可作为 workspace support 项参与临时 `go.work`。

## 架构约定（Core 模块）

架构说明见 [框架架构](docs/architecture/framework-design.md)。本项目不保留门禁测试代码，以下约束由代码评审执行；**新增顶级包或跨能力域依赖边，必须先修订本矩阵并说明架构理由，再写代码**。

1. **顶级目录白名单**：`app audit auth cache clock codec contextx db domain errors eventing gen httpx internal messaging observe policy process testkit validate scripts examples`（`examples` 仅存放示例 `main` 包，生产代码禁止 import）。
2. **能力域依赖矩阵**（生产代码只允许下列 gochen 内部依赖，标准库不受限）：

   | 包                               | 允许依赖（gochen 内部）                                                                               |
   | -------------------------------- | ----------------------------------------------------------------------------------------------------- |
   | audit                            | domain, errors |
   | app                              | audit, auth（仅根契约）, clock, codec, contextx, domain, errors, eventing, gen, internal, messaging, observe, policy, validate |
   | auth（声明编译、策略、统一授权与治理；子包见 `auth/doc.go`） | clock, contextx, domain, errors, gen, internal, observe                                               |
   | cache                            | clock                                                                                                 |
   | clock / codec / gen / validate   | errors（基础叶子，彼此禁止互相 import，协作用最小结构接口注入）                                       |
   | contextx                         | errors, gen                                                                                           |
   | db                               | contextx, errors                                                                                      |
   | domain                           | errors                                                                                                |
   | errors                           | （无）                                                                                                |
   | eventing                         | clock, codec, contextx, db, errors, gen, internal, messaging, observe, policy, process                |
   | internal                         | errors（基础辅助不得依赖上层能力域）                                                 |
   | httpx                            | contextx, errors                                                                                      |
   | messaging                        | clock, contextx, errors, internal, observe, validate                                                  |
   | observe                          | contextx                                                                                              |
   | policy                           | clock, errors                                                                                         |
   | process                          | clock, errors, eventing, gen, internal, messaging, observe, policy                                    |
   | testkit                          | audit, app, auth/scoped, contextx, db, domain, errors, httpx, gen                                            |

   `testkit → auth/scoped` 用于内存仓储复现 L3 写入约束、租户对齐与版本检查，供无数据库的授权链路测试使用；该依赖不扩展业务包的允许矩阵。

3. **依赖地板**：`domain` 非测试代码只依赖标准库与 `gochen/errors`（及自身子树）；基础叶子（clock/codec/gen/validate）只依赖标准库、`gochen/errors` 与本包子树。
4. **应用授权契约依赖**：按 D15，`app/crud` / `app/crud/audit` / `app/eventsourced` 通过 `auth` 根包的 `IAuthorizer` 编排授权。依赖严格单向；`auth` 禁止 import `app`，基础应用禁止依赖 `auth/action`、`auth/scoped`、`auth/governance`。`domain/meta`、`domain/crud` 的元数据和仓储能力接口沿用 domain 子树依赖地板。
5. **安全能力落点**：Application 在用例入口执行授权，把 `spec.Spec` 显式交给 Repository；Repository 构造期用 `WithIsolation` / `WithResourceKind` 声明隔离与资源。可选能力由仓储本体提供，禁止包装 `IRepository`。具体契约见[分层授权](docs/architecture/layered-authz.md)。

   授权声明与治理编排集中于 `auth` 根包，组合根编译强类型声明并注入 `IAuthorizer`；根包可以依赖纯权限码工具子包 `auth/action`。`auth/scoped` 保留 DataScope 值对象与绑定，依赖根契约；范围资料由提供方投影到根包范围契约。此组织将声明校验和执行语义集中到同一模块，保持 Core 零第三方依赖及 `app → auth` 单向消费。

6. **具体协作边**：`eventing → process` 仅允许 `eventing/outbox → process/task`；`process → eventing` 仅允许 `process/saga → eventing` / `eventing/bus`。任务监督不反向依赖事件能力。
7. **默认适配分包**：`app/eventsourced` 保留领域仓储与命令用例；`app/eventsourced/eventstore` 承载事件持久化/快照/历史适配，`app/eventsourced/subscriber` 承载事件处理/投影/注册。核心模板禁止反向依赖这两个可选子包，组合根负责装配。
8. **CRUD 生命周期与可选历史**：删除生命周期归入 `app/crud.Application`，按构造期必填 DeleteMode 冻结软删或完整永久清除流程；恢复与已删除查询独立选配。审计历史读取独立位于 `app/crud/audit`，`app/crud` 禁止反向依赖该子包。审计记录与存储契约继续位于顶级 `audit`，通用执行骨架继续位于 `app/internal/execution`，不因目录分组迁入 CRUD 子树。

审计记录和维度快照独立归入 audit；domain 只保留可选字段与状态契约，不依赖 audit。
