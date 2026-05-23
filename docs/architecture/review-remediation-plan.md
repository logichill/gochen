# 架构评审后续整改计划

本文只记录尚未落地的整改项；已完成的 `domain/access` 授权边界迁移、`ScopeModeScoped = "scoped"`、Makefile 全仓 `race/coverage`、CI `race/coverage` 不再重复列入。

## 处理原则

- 先改边界最清晰、收益确定的项；涉及公共 API 形态或运行时语义的项先形成方案再改代码。
- 重构不保留兼容层，除非下游迁移成本明显高于收益并另行确认。
- 每个整改项完成时同步更新本页状态，并补充验证命令。

## P1：建议优先落地

### 1. `db/query` 去除对 `domain` 的依赖

**状态**：已完成（2026-05-14）。`IQueryableRepository` 已改为 `T any` 约束，`db/query` 生产代码不再依赖 `gochen/domain`。

**现状**：`db/query/options.go` 曾仅为 `IQueryableRepository[T domain.IEntity[ID], ID comparable]` 引入 `gochen/domain`。

**问题**：`db/query` 是查询 DSL / 查询契约包，不应为了一个仓储扩展接口反向依赖领域实体包；这会让基础查询能力与领域模型绑定，增加下游复用成本。

**建议方案**：

- 将 `IQueryableRepository` 的泛型约束从 `domain.IEntity[ID]` 降为 `T any`，因为接口方法本身不使用 `GetID/GetVersion`。
- 如确实需要实体约束，由调用方或 `domain/crud`、`app/crud` 在自身包内声明更窄接口，而不是让 `db/query` 承担领域约束。
- 验收扫描：`rg -n 'gochen/domain|domain\.' db/query` 无生产代码命中。

**验证命令**：

```sh
cd /home/alex/data/priv/project/gochen && rg -n 'gochen/domain|domain\.' db/query -g '*.go'
cd /home/alex/data/priv/project/gochen && go test ./db/query ./app/crud ./api/rest ./db/orm/repo -count=1
cd /home/alex/data/priv/project/gochen && go test ./... -count=1 && go vet ./...
```

**验证结果**：2026-05-14 通过；依赖扫描无命中。

### 2. `httpx` 与 `auth` 的传输层耦合收敛

**状态**：已完成（2026-05-14）。`httpx` 已改为直接依赖 `contextx` 读写标准上下文字段，生产代码与测试均不再导入 `gochen/auth`。

**现状**：`httpx/tenant.go`、`httpx/middleware/security_layer.go`、`httpx/middleware/access_log.go` 曾直接导入 `gochen/auth`，主要用于 tenant/session/user/operator 等上下文读写。

**问题**：`httpx` 是协议抽象层，直接绑定 `auth` 会让 HTTP 基础能力默认携带 gochen auth 运行时假设；未来如果下游使用不同认证方案，替换成本偏高。

**建议方案**：

- 短期：保留当前默认中间件，但把 auth-aware 组件标注为“gochen auth adapter”，避免把它们当作纯 `httpx` 核心能力。
- 中期：在 `httpx` 定义最小上下文绑定接口，例如 `TenantBinder`、`SessionVisibilityBinder`、`AccessLogContextExtractor`；默认实现放在 `auth/http` 或 `httpx/middleware/authadapter`。
- 迁移后 `httpx` 核心包只依赖 `contextx` 或接口，不直接依赖 `auth`；auth 适配层负责把 principal/session/tenant 投影进请求上下文。

**验收扫描**：

```sh
cd /home/alex/data/priv/project/gochen && rg -n 'gochen/auth|auth\.' httpx -g '*.go'
cd /home/alex/data/priv/project/gochen && go test ./httpx ./httpx/middleware ./httpx/nethttp ./auth/http -count=1
cd /home/alex/data/priv/project/gochen && go test ./... -count=1 && go vet ./...
```

**验证结果**：2026-05-14 通过；`httpx` 全目录扫描无命中。

### 3. 生命周期方法命名收敛

**状态**：已完成（2026-05-14）。后台服务主入口已收敛为 `Start(ctx)` / `Stop(ctx)`，阻塞主循环保留 `Run(ctx)` / `Shutdown(ctx)`，资源型对象继续使用 `Close()`。已同步下游 6 个项目。

**现状**：项目曾同时存在 `Start/Stop`、`Start/Close`、`Run/Shutdown` 三类生命周期口径，例如 HTTP server、transport、outbox publisher、host runtime、task supervisor。

**目标约定**：

| 场景 | 推荐命名 | 说明 |
| --- | --- | --- |
| 后台服务、可启动/停止组件 | `Start(ctx)` / `Stop(ctx)` | 非阻塞启动，停止需要等待后台协程收尾 |
| 一次性资源、I/O handle | `Open` / `Close` 或仅 `Close` | 与标准库资源语义一致 |
| 阻塞式主循环或进程级 runtime | `Run(ctx)` / `Shutdown(ctx)` | `Run` 持有主循环，`Shutdown` 从外部请求终止 |

**建议方案**：

- 先补文档和接口注释，明确哪些类型属于服务、资源或主循环。
- 对新代码强制按上表命名；旧代码分批迁移，不在同一类型上同时暴露多套同义生命周期 API。
- `Close()` 可作为资源接口适配保留，但不应成为后台服务的主关闭入口；如果保留，必须只是 `Stop(contextx.Background())` 的薄包装。

**验收扫描**：

```sh
cd /home/alex/data/priv/project/gochen && rg -n 'StopWithTimeout|ITransportCloseWithContext|CloseWithContext|CloseWithTimeout|transport\.Close\(|tpt\.Close\(|state\.Transport\.Close\(|func \(.*Transport\) Close\(' . -g '*.go'
cd /home/alex/data/priv/project/gochen && rg -n 'func .*\b(Start|Stop|Close|Run|Shutdown)\(' --glob '*.go'
cd /home/alex/data/priv/project/gochen && go test ./... -count=1 && go vet ./...
cd /home/alex/data/priv/project/gochen-starter && go test ./... -count=1 && go vet ./...
for d in /home/alex/data/priv/project/{gochen-iam,gochen-llm,alife,ems,erp}; do (cd "$d" && go test ./... -count=1 && go vet ./...); done
```

**验证结果**：2026-05-14 通过；旧 transport `Close/CloseWithContext` 与 `StopWithTimeout` 扫描无命中，保留的 `Close()` 均为资源型对象或标准库适配。

### 4. `app/crud` Hook 机制二选一或明确边界

**状态**：已完成（2026-05-15）。`app/crud` 已收敛为 Hooks-only：CRUD 写入扩展只通过 `Hooks` 结构体显式注入，不再暴露 `BeforeCreate`、`AfterCreate` 等方法 fallback。

**现状**：`app/crud` 曾同时支持 `Hooks` 结构体函数字段和可覆盖方法（`BeforeCreate`、`AfterCreate` 等），运行时优先显式 hooks，再 fallback 到方法实现。

**问题**：两套扩展点表达同一件事，认知成本高；如果调用方同时设置两套机制，实际执行顺序需要读源码才能确认。

**建议方案**：

- 推荐方向：保留 `Hooks` 结构体作为组合根显式装配入口；方法覆盖机制降级为兼容旧派生类型的 advanced 用法，并在文档中声明优先级。
- 若选择彻底收敛：删除方法 fallback，只保留 `Hooks`；下游自定义逻辑通过构造时注入 hooks。
- 代码变更前需要确认是否接受 API 破坏面；确认后同步 `app/README.md` 与 `docs/guides/downstream-guide.md`。

**验收标准**：Hook 文档能回答“推荐用哪种、两者同时存在谁生效、何时不建议使用”。

**验证命令**：

```sh
cd /home/alex/data/priv/project/gochen && rg -n 'IHooks\[|return s\.(Before|After)|func \(s \*Application.*\) (Before|After)(Create|Update|Delete)|回退到方法实现' app/crud api/rest -g '*.go'
cd /home/alex/data/priv/project/gochen && go test ./app/crud ./app/audited ./api/rest -count=1
cd /home/alex/data/priv/project/gochen && go test ./... -count=1 && go vet ./...
for d in /home/alex/data/priv/project/{gochen-starter,gochen-iam,gochen-llm,alife,ems,erp}; do (cd "$d" && go test ./... -count=1 && go vet ./...); done
```

**验证结果**：2026-05-15 通过；旧方法 fallback 扫描无命中，下游仍通过 `Hooks` 显式注册业务扩展。

## P2：中期治理项

### 5. 并发原语使用约定文档化

**状态**：已完成（2026-05-16）。并发原语使用边界已补入 `docs/architecture/framework-design.md` 的 `1.4 并发原语使用约定`，作为新增共享状态与评审门禁的统一口径。

**现状**：热替换 recorder/metrics 多处使用 `atomic.Value`，生命周期状态、registry、内存 store 使用 `sync.Mutex/RWMutex`。

**约定**：

- `atomic.Value` 仅用于读多写少、单槽位热替换、动态类型固定的配置/recorder/metrics 指针。
- `sync.Mutex` 用于多字段不变量、生命周期状态迁移、channel close 保护、一次发布流程串行化。
- `sync.RWMutex` 用于 registry/cache/store 等读多写少的 map/slice 状态。
- 不为追求“无锁”替换 mutex；涉及多个字段一致性时优先 mutex。

**验证命令**：

```sh
cd /home/alex/data/priv/project/gochen && rg -n '并发原语|atomic.Value|sync.Mutex|sync.RWMutex|无锁' docs/architecture/framework-design.md docs/architecture/review-remediation-plan.md
cd /home/alex/data/priv/project/gochen && git diff --check
```

**验证结果**：2026-05-16 通过；本项仅变更架构文档，未运行 Go 测试。

### 6. CI 补齐 lint、覆盖率门禁与 Go 版本矩阵

**状态**：已完成（2026-05-16）。CI 已拆分为 lint、test matrix、race、coverage 四类 job，并补充 `golangci-lint`、覆盖率阈值门禁和 Go 版本矩阵。

**现状**：CI 已包含 `gofmt`、`go vet`、`go test ./...`、`go test -race ./...`、覆盖率生成；现在新增 `golangci-lint` 增量门禁、`COVERAGE_MIN` 覆盖率阈值和 `1.26.x` / `stable` 测试矩阵。

**落地方案**：

- 新增 `.golangci.yml`，以 `govet`、`staticcheck`、`errcheck` 为初始集合，并通过增量检查避免一次性暴露历史噪音。
- `.github/workflows/ci.yml` 拆分 job：`lint` 负责格式/静态分析，`test` 使用 `1.26.x` 与 `stable` 矩阵，`race` 独立运行竞态检测，`coverage` 独立生成并检查覆盖率。
- `Makefile` 新增 `lint` 与 `coverage-check`，`COVERAGE_MIN ?= 1` 作为当前最低非侵入门槛，后续可按包分组逐步提高。

**验收标准**：CI 能区分格式、静态分析、单测、竞态、覆盖率门禁失败原因。

**验证命令**：

```sh
cd /home/alex/data/priv/project/gochen && GOPROXY=https://proxy.golang.org,direct go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 config verify
cd /home/alex/data/priv/project/gochen && GOPROXY=https://proxy.golang.org,direct go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --new ./...
cd /home/alex/data/priv/project/gochen && make coverage-check
cd /home/alex/data/priv/project/gochen && python3 - <<'PY'
import yaml
for path in ['.github/workflows/ci.yml', '.golangci.yml']:
    with open(path, 'r', encoding='utf-8') as f:
        yaml.safe_load(f)
PY
cd /home/alex/data/priv/project/gochen && git diff --check
```

**验证结果**：2026-05-16 通过；`make coverage-check` 全仓测试通过，总覆盖率 58.2%，高于当前 `COVERAGE_MIN=1` 门槛。

### 7. 集成测试 build tag 规则

**状态**：已完成（2026-05-16）。测试分层与 `integration` build tag 规则已补入 `docs/architecture/framework-design.md`，并在 `Makefile` 增加 `make integration` 统一入口。

**现状**：当前未发现必须依赖真实 DB、网络或外部服务的测试；多数 MySQL/Postgres 相关测试是 SQL 渲染或 fake DB，SQLite 测试使用内存库或 `t.TempDir()` 临时文件，不应机械加 `integration`。

**规则**：

- 凡是依赖真实外部资源（DB、Redis、网络服务、固定文件路径、Docker/testcontainers）的测试，必须使用 `//go:build integration`。
- 默认 `go test ./...` 只跑单元测试、纯内存测试、`httptest`、`t.TempDir()` 和内存/临时 SQLite 测试。
- 集成测试入口统一为 `go test -tags=integration ./...` 或 `make integration`，后续可在 CI 中作为可选 job 或 nightly job。

**验证命令**：

```sh
cd /home/alex/data/priv/project/gochen && rg -n '^//go:build integration|sql\.Open\(|net\.Listen\(|testcontainers|docker|redis' . -g '*_test.go'
cd /home/alex/data/priv/project/gochen && rg -n 'integration|测试分层|go test -tags=integration|make integration' docs/architecture/framework-design.md Makefile
cd /home/alex/data/priv/project/gochen && make -n integration
cd /home/alex/data/priv/project/gochen && git diff --check
```

**验证结果**：2026-05-16 通过；扫描未发现真实外部 DB、Redis、Docker/testcontainers 或固定端口监听测试需要迁移到 `integration`。

### 8. `api/rest.RouteConfig` 按关注点分组

**状态**：已完成（2026-05-16）。`RouteConfig[ID]` 已按关注点拆为 `Routing`、`Query`、`Body`、`HTTP`、`Response`、`Audit` 与 `Authorization`，并同步 6 个下游项目调用点。

**现状**：`RouteConfig[ID]` 顶层只保留分组字段；路由开关与 ID codec 归入 `Routing`，分页/query schema/白名单归入 `Query`，请求体限制与 API 校验归入 `Body`，CORS 与路由中间件归入 `HTTP`，响应包装与错误处理归入 `Response`，审计 operator 提取归入 `Audit`，授权配置保持独立。

**问题**：单个配置结构过大，新增能力时容易继续膨胀；调用方难以区分哪些配置影响 HTTP 基础行为，哪些影响 CRUD 查询、审计或授权。

**落地方案**：

- 保留 `rest.Register` / `ApiBuilder.Route` / `WithPagination` / `WithAuthorization` 等使用入口，但 `RouteConfig` 直接字段写入统一迁移到分组字段。
- 不保留旧顶层字段兼容层，避免继续扩大公共配置面。
- 同步更新 `api/rest/README.md`、`docs/architecture/framework-design.md`、本仓示例和 6 个下游调用点。

**验收标准**：新配置项能自然归入某个子配置；`RouteConfig` 顶层字段数不再随能力增长线性膨胀。

**验证命令**：

```sh
cd /home/alex/data/priv/project/gochen && rg -n '(cfg|rc)\.(BasePath|IDCodec|EnableList|EnableGet|EnableCreate|EnableUpdate|EnableDelete|EnableBatch|EnablePagination|Validator|ErrorHandler|MaxPageSize|DefaultPageSize|MaxBodySize|AllowedFilterFields|AllowedSortFields|AllowedFields|QuerySchemaInferOptions|CORS|Middlewares|ResponseWrapper|UseHTTP201ForCreate|OperatorExtractor)' api/rest examples docs -g '*.go' -g '*.md'
for d in /home/alex/data/priv/project/{gochen-starter,gochen-iam,gochen-llm,alife,ems,erp}; do (cd "$d" && rg -n '(cfg|rc)\.(BasePath|IDCodec|EnableList|EnableGet|EnableCreate|EnableUpdate|EnableDelete|EnableBatch|EnablePagination|Validator|ErrorHandler|MaxPageSize|DefaultPageSize|MaxBodySize|AllowedFilterFields|AllowedSortFields|AllowedFields|QuerySchemaInferOptions|CORS|Middlewares|ResponseWrapper|UseHTTP201ForCreate|OperatorExtractor)' . -g '*.go' -g '!.*'); done
cd /home/alex/data/priv/project/gochen && go test ./... -count=1 && go vet ./...
for d in /home/alex/data/priv/project/{gochen-starter,gochen-iam,gochen-llm,alife,ems,erp}; do (cd "$d" && go test ./... -count=1 && go vet ./...); done
cd /home/alex/data/priv/project/gochen && git diff --check
for d in /home/alex/data/priv/project/{gochen-starter,gochen-iam,gochen-llm,alife,ems,erp}; do (cd "$d" && git diff --check); done
```

**验证结果**：2026-05-16 通过；旧顶层字段写入扫描无命中，本仓与 6 个下游均通过 `go test ./... -count=1 && go vet ./...`，本仓和下游 `git diff --check` 通过。

### 9. `logging` 与 ID 生成器的传递依赖拆分

**状态**：已完成（2026-05-16）。已新增 `contextx/fields` 轻量字段子包，`logging.ContextFields` 改为只依赖 `contextx/fields`，不再传递依赖 `ident/snowflake`。

**现状**：`logging/context_fields.go` 只读取标准字段；`contextx.Background()` / `GenerateTraceID()` 仍保留在 `contextx` 根包并依赖 `ident/snowflake`，但 `logging` 不再导入 `contextx` 根包。

**问题**：日志包只需要上下文字段 key 与读取函数，不需要承担 trace id 生成能力的传递依赖。

**落地方案**：

- `contextx/fields` 承载 `tenant_id`、`trace_id`、`request_id`、`operator` 的 key 与 context accessor。
- `contextx` 根包保留原有 `WithTraceID` / `TraceID` / `WithTenantID` / `TenantID` 等 API，内部转发到 `contextx/fields`，避免扩大调用方迁移面。
- `logging` 直接依赖 `contextx/fields`，只消费字段读取能力。

**验证命令**：

```sh
cd /home/alex/data/priv/project/gochen && go list -deps ./contextx/fields | rg 'gochen/ident|snowflake'
cd /home/alex/data/priv/project/gochen && go list -deps ./logging | rg 'gochen/ident|snowflake'
cd /home/alex/data/priv/project/gochen && go test ./contextx ./contextx/fields ./logging -count=1
cd /home/alex/data/priv/project/gochen && go test ./... -count=1 && go vet ./...
cd /home/alex/data/priv/project/gochen && git diff --check
```

**验证结果**：2026-05-16 通过；`contextx/fields` 与 `logging` 依赖扫描均无 `gochen/ident` / `snowflake` 命中。

### 10. Outbox 串行/并行 publisher 收敛

**状态**：已完成（2026-05-16）。已新增 `eventing/outbox/publisher_core.go`，串行 `Publisher` 与并行 `ParallelPublisher` 共享 claim、decode/upcast、publish、mark success/failure、DLQ、metrics、cleanup 的内部核心；两个公开构造函数继续保留为串行/并行入口，调度与生命周期仍按各自模型维护。

**现状**：`eventing/outbox` 同时存在 `Publisher` 与 `ParallelPublisher`；两者共享 repo/bus/config/registry/upgrader/DLQ/metrics，发布语义已收敛到同一份 `outboxPublisherCore`，差异仅保留在串行 loop、并行 worker pool、分片分发与批量 mark 调度。

**落地方案**：

- `outboxPublisherCore` 统一 claim、反序列化/升级、发布、指标记录、成功标记、失败标记、DLQ 迁移与已发布清理。
- `Publisher.processOnce` 与 `ParallelPublisher.processEntry/fetchOnce/cleanupLoop` 调用同一核心，避免发布行为 bugfix 双写。
- 并行特有能力（worker pool、按 aggregate 分片、批量 mark、停止 drain）保留在 `ParallelPublisher`，不与串行生命周期强行合并。

**验收标准**：串行与并行路径共享同一份发布/失败/DLQ/metrics 核心逻辑；新增 outbox 行为只需改一个核心实现。

**验证命令**：

```sh
cd /home/alex/data/priv/project/gochen && go test ./eventing/outbox -count=1
cd /home/alex/data/priv/project/gochen && go test -race ./eventing/outbox -count=1
cd /home/alex/data/priv/project/gochen && go test ./... -count=1 && go vet ./...
cd /home/alex/data/priv/project/gochen && git diff --check
```

**验证结果**：2026-05-16 通过；专项 `go test ./eventing/outbox -count=1`、`go test -race ./eventing/outbox -count=1` 通过，全仓 `go test ./... -count=1 && go vet ./...` 与 `git diff --check` 通过。

## 暂不列入整改

- `db/orm/repo -> auth`：已通过 `domain/access` contract 和 `auth` 投影函数迁移完成。
- `Makefile coverage/race 只覆盖 4 个包`：已调整为全仓目标，核心包目标独立保留为 `*-core`。
- CI 缺少 `race` 与覆盖率报告：已补齐；剩余仅为 lint、门禁和矩阵增强。
