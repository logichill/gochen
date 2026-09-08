# AGENTS 准则（Gochen Core）

本仓库为 Gochen Core（module `gochen`），是 Gochen 框架的契约层。配套 Runtime 位于独立仓库：

- 本仓库 `gochen`（Core）：领域、应用、认证、事件、消息、策略、流程与 HTTP 抽象；规范见 `SPEC.md`
- `../gochen-runtime`（Runtime，module `gochen-runtime`）：Core 的物理驱动层——SQL 实现、net/http server、config、host 容器、api/rest；局部约定见该仓库 `AGENTS.md`
- `examples/`：仅依赖 Core 的可运行示例；需要 Runtime 的示例位于 `gochen-runtime` 仓库 `examples/`

## 通用准则

- 这是 golang 项目，需要在遵守 `SPEC.md` 的基础上遵守 golang 编码规范
- 执行任何重构都不要保留兼容层代码，保持项目的干净清爽
- 执行代码创建/修改后要执行 go fmt
- 默认验证：**各仓库内**执行 `go build ./... && go vet ./... && go test -count=1 ./...`；全量入口见 `Makefile`。禁止退化为 workspace 级一把梭，否则 Core 零依赖不变量会被掩盖。

## 命名铁律

- **repo 名 = module path = 目录名**，三者必须一致（当前：`gochen` / `gochen-runtime`）。
- module path **禁止使用 Go 标准库顶级包名**，避免标准库与本地模块导入冲突。新增 module 前必须核对 `go list std | rg -x '<候选名>'` 为空。
- 生态内 module 一律 `gochen` 或 `gochen-*` 前缀，平级 dotless。

## 模块边界与依赖

- 依赖硬规则：只允许 `gochen-runtime → gochen` 单向依赖，禁止任何反向引用；Core 任何文件（含测试与示例）禁止 import `gochen-runtime`；SQL、配置加载、net/http server、Host/DI 属于 Runtime。
- Core 生产与测试代码均实现零外部第三方依赖；`go.mod` 无任何直接或间接第三方 require 项，且不应存在 `go.sum`。门禁校验：在本仓库执行 `GOWORK=off go list -m all` 输出必须只有 `gochen` 自身。
- Runtime 第三方依赖仅限物理驱动所需（sqlite/yaml/uuid/testify）。
- 开发态由 workspace 级 `go.work`（位于两仓库的父目录）组合；Runtime 发布态必须改为真实 Core 版本，不依赖本地 replace。

## 下游项目

有且仅有以下项目为本项目的下游：

- ../gochen-runtime
- ../gochen-contrib
- ../gochen-iam
- ../gochen-llm
- ../gochen-workflow
- ../alife
- ../ems
- ../erp

核心三包为: gochen, gochen-runtime, gochen-contrib。

`../gochen-workflow` 不是本项目下游测试目标；仅当下游本地 replace/workspace 需要时，可作为 workspace support 项参与临时 `go.work`。

## 架构约定（Core 模块）

架构说明见 [框架架构](docs/architecture/framework-design.md)。本项目不保留门禁测试代码，以下约束由代码评审执行；**新增顶级包或跨能力域依赖边，必须先修订本矩阵并说明架构理由，再写代码**。

1. **顶级目录白名单**：`app auth cache clock codec contextx db domain errors eventing gen httpx internal messaging observe policy process testkit validate scripts examples`（`examples` 仅存放示例 `main` 包，生产代码禁止 import）。
2. **能力域依赖矩阵**（生产代码只允许下列 gochen 内部依赖，标准库不受限）：

   | 包                               | 允许依赖（gochen 内部）                                                                               |
   | -------------------------------- | ----------------------------------------------------------------------------------------------------- |
   | app（`app/security/*` 除外）     | clock, codec, contextx, domain, errors, eventing, gen, internal, messaging, observe, policy, validate |
   | app/security/*                   | 上行全部 + auth/{action, scoped}                                                                      |
   | auth（能力子包见 `auth/doc.go`） | clock, contextx, domain, errors, gen, internal, observe                                               |
   | cache                            | clock                                                                                                 |
   | clock / codec / gen / validate   | errors（基础叶子，彼此禁止互相 import，协作用最小结构接口注入）                                       |
   | contextx                         | errors, gen                                                                                           |
   | db                               | contextx, errors                                                                                      |
   | domain                           | errors                                                                                                |
   | errors                           | （无）                                                                                                |
   | eventing                         | clock, codec, contextx, db, errors, gen, internal, messaging, observe, policy, process                |
   | httpx                            | contextx, errors                                                                                      |
   | messaging                        | clock, contextx, errors, internal, observe, validate                                                  |
   | observe                          | contextx                                                                                              |
   | policy                           | clock, errors                                                                                         |
   | process                          | clock, errors, eventing, gen, internal, messaging, observe, policy                                    |
   | testkit                          | app, contextx, db, domain, errors, httpx, gen                                                         |

3. **依赖地板**：`domain` 非测试代码只依赖标准库与 `gochen/errors`（及自身子树）；基础叶子（clock/codec/gen/validate）只依赖标准库、`gochen/errors` 与本包子树。
4. **`app/security` 单向依赖 `auth`**：`app/security/*` 是应用层安全 PEP 装饰器，其职责即把 `auth` 的安全契约应用到 Application 编排中，故允许 `app/security → auth`。该边**严格单向**（`auth` 禁止 import 任何 `app` 包），且基础业务包 `app/crud` / `app/audited` / `app/eventsourced` 必须保持对 `auth` 零依赖。
5. **安全能力落点**：装饰器只用于接口宽的 Application 层；Repository 层（`domain/crud.IRepository` 仅 4 个方法，其余能力靠类型断言探测）一律用**构造期显式选项**表达安全约束，禁止包装 `IRepository`——否则可选能力接口会丢失或被假冒。具体契约见[分层授权](docs/architecture/layered-authz.md)。
