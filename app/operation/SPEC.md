# app/operation SPEC

> 本文档定义 `app/operation` 的第一阶段与完整阶段设计规格，用于约束实现方向，降低跨仓执行漂移。

## 1. 文档目标

`app/operation` 不是“讨论用 README”，而是后续实现时的硬约束文档。

本文档回答以下问题：

- `Operation` 的职责边界是什么；
- `inline / tracked` 如何判定；
- 统一返回结构长什么样；
- `app/crud` 与 `app/eventsourced` 应如何接入；
- 哪些能力第一阶段必须做，哪些能力后置；
- 哪些设计明确禁止，避免系统变重。

## 2. 核心定义

### 2.1 基本语义

- `Command`：写侧意图，表达“系统要做什么”。
- `Operation`：这次写侧意图执行后的可观察运行时外壳，表达“系统现在执行到哪了”。
- `Query / ReadModel`：结果的可见面，表达“外部当前能读到什么”。

结论：

> `Operation = 可观察的 Command 执行过程`

### 2.2 适用范围

`app/operation` 适用于所有“写入口”：

- CRUD Application 的写操作；
- EventSourced Application 的命令执行；
- 其他自定义 Application Service 的写操作。

它不要求调用方必须显式使用 `CommandBus`。

## 3. 设计原则

### 3.1 统一协议，不统一技术实现

统一的是：

- 写操作返回协议；
- 写操作执行状态语义；
- 写操作影响范围表达；
- 写操作跟踪与收敛入口。

不统一的是：

- 是否一定使用 `CommandBus`；
- 是否一定使用事件溯源；
- 是否一定是异步任务；
- 是否一定需要状态持久化。

### 3.2 默认轻量，按需升级

默认路径必须足够轻。

- 普通同步写操作默认 `inline`
- 只有在“结果延迟可见 / 需要等待收敛 / 需要公开处理中状态”时才升级为 `tracked`

### 3.3 `tracked` 判定依据

`tracked` 判定不取决于“是否使用了 CQRS / Command 模式”，而只取决于：

- 写侧返回成功后，结果是否仍延迟可见；
- 是否存在明确的收敛条件需要等待；
- 是否需要对外暴露处理中状态。

因此：

- `CRUD` 默认通常为 `inline`
- `EventSourced / CQRS` 默认也通常为 `inline`
- 只有“写后结果延迟可见”的操作才应是 `tracked`

## 4. 第一阶段范围

第一阶段只要求实现：

- 核心模型：
  - `Mode`
  - `Status`
  - `Spec`
  - `Result`
  - `Runner`
- `inline` 完整可用
- `tracked` 协议占位可用
- `app/crud` 与 `app/eventsourced` 可选接入点

第一阶段不要求完整实现：

- SQL 持久化 operation store
- SSE
- polling fallback
- 复杂 settlement DSL
- 全量幂等中心

## 5. 核心模型

### 5.1 Mode

```go
type Mode string

const (
    ModeInline  Mode = "inline"
    ModeTracked Mode = "tracked"
)
```

语义：

- `inline`
  - 同步完成；
  - 无需后续状态跟踪；
  - 响应返回时即视为 `settled` 或 `failed`。
- `tracked`
  - 已接受，但后续仍需跟踪；
  - 可能需要等待收敛；
  - 可暴露状态查询与状态流。

### 5.2 Status

```go
type Status string

const (
    StatusAccepted   Status = "accepted"
    StatusProcessing Status = "processing"
    StatusSettled    Status = "settled"
    StatusFailed     Status = "failed"
    StatusTimeout    Status = "timeout"
    StatusDegraded   Status = "degraded"
)
```

第一阶段要求：

- `inline` 至少可返回 `settled` / `failed`
- `tracked` 至少可返回 `accepted`

完整阶段要求：

- 支持 `processing / timeout / degraded`

### 5.3 Resource

```go
type Resource struct {
    Type string
    ID   string
}
```

要求：

- 允许为空；
- 如果当前写操作天然绑定资源，应尽量填写；
- `Type` 使用中立名词，如 `task` / `user` / `order`。

### 5.4 Spec

`Spec` 是 `Runner` 执行写操作时的输入规格。

建议最小字段：

```go
type Spec struct {
    Type            string
    Mode            Mode
    Resource        *Resource
    AffectedScopes  []string
    IdempotencyKey  string
    Metadata        map[string]any
    SettlementHint  any
}
```

字段约束：

- `Type`
  - 必填；
  - 表达业务动作，如 `task.create`、`task.complete`
- `Mode`
  - 必填；
  - 由业务明确声明，不自动猜测
- `AffectedScopes`
  - 可为空，但推荐填写；
  - 由业务层定义，不由框架硬编码
- `IdempotencyKey`
  - 第一阶段可选；
  - 预留后续幂等支持
- `SettlementHint`
  - 第一阶段可选；
  - 作为后续 tracked 收敛扩展位

### 5.5 Result

后端统一返回结构应至少包含：

```go
type Result struct {
    Operation Operation
    Resource  *Resource
    Result    map[string]any
    Error     *OperationError
    AffectedScopes []string
    StatusURL string
    StreamURL string
    RetryAfterMs int
}
```

其中 `Operation` 至少包含：

```go
type Operation struct {
    ID     string
    Type   string
    Mode   Mode
    Status Status
}
```

约束：

- `inline`
  - 可以没有真实持久化 `ID`
  - 但对外仍建议保留 `operation` 节点
- `tracked`
  - 必须有可查询的 `ID`

### 5.6 Runner

第一阶段推荐的最小抽象：

```go
type Runner interface {
    Execute(ctx context.Context, spec *Spec, fn func(ctx context.Context) (*Result, error)) (*Result, error)
}
```

语义要求：

- `Runner` 是 application service 边界上的包装器；
- `fn` 才是真正的业务写逻辑；
- `Runner` 负责：
  - 校验 `spec`
  - 注入 operation 元数据
  - 统一返回 envelope
  - 对 `inline` / `tracked` 做模式分流

禁止：

- 让 `fn` 自己组装完整 operation envelope；
- 让业务代码直接操作 tracked 状态机底层实现。

## 6. JSON 契约

### 6.1 inline 成功

```json
{
  "code": "ok",
  "message": "success",
  "data": {
    "operation": {
      "type": "task.create",
      "mode": "inline",
      "status": "settled"
    },
    "resource": {
      "type": "task",
      "id": "426380895038083073"
    },
    "result": {
      "task_id": "426380895038083073"
    },
    "affected_scopes": [
      "tasks:list",
      "dashboard:summary"
    ]
  }
}
```

### 6.2 tracked 已接受

```json
{
  "code": "ok",
  "message": "accepted",
  "data": {
    "operation": {
      "id": "op_123",
      "type": "task.complete",
      "mode": "tracked",
      "status": "accepted"
    },
    "resource": {
      "type": "task",
      "id": "426380895038083073"
    },
    "affected_scopes": [
      "tasks:list",
      "tasks:detail:426380895038083073",
      "points:5",
      "levels:5"
    ],
    "status_url": "/api/v1/operations/op_123",
    "stream_url": "/api/v1/operations/op_123/stream",
    "retry_after_ms": 800
  }
}
```

### 6.3 失败

```json
{
  "code": "VALIDATION_ERROR",
  "message": "title is required",
  "data": {
    "operation": {
      "type": "task.create",
      "mode": "inline",
      "status": "failed"
    }
  }
}
```

## 7. 模板接入要求

### 7.1 app/crud

接入要求：

- `Create / Update / Delete` 可选使用 `Runner`
- 默认 `ModeInline`
- 不启用 operation 时，旧行为必须保持不变

禁止：

- 强制所有 CRUD 调用方修改代码才能继续工作

### 7.2 app/eventsourced

接入要求：

- `ExecuteCommand` 可选使用 `Runner`
- 默认不因为“使用命令模式”自动升级 tracked
- 业务可显式声明某个命令为 tracked

禁止：

- 直接侵入 `CommandBus` 核心
- 把 `Operation` 做成 `Command` 的替代入口

## 8. tracked 完整阶段要求

完整阶段允许扩展：

- `store`
- `settlement`
- `stream`
- `http`

完整阶段需要支持：

- operation 持久化
- 状态查询
- SSE 推送
- 轮询降级所需协议字段
- settlement condition
- timeout / degraded

但这些都不是第一阶段阻断项。

## 9. 非目标

当前明确不是目标的内容：

- 重写 `messaging/command`
- 重写 `CommandBus`
- 替换 `workflow` / `saga`
- 第一阶段引入完整 durable workflow
- 第一阶段一次性完成所有 tracked 基础设施

## 10. 禁止项

实现时必须避免：

- 把所有 command 默认当成 tracked
- 把 tracked 判定和 CQRS 风格绑定
- 让 CRUD 也必须经过重型 operation store
- 在第一阶段就引入过量子包与 DSL
- 让前端直接依赖后端具体内部 settlement 细节

## 11. 第一阶段验收标准

必须满足：

- `app/operation` 最小接口存在；
- `inline` 写操作可统一返回 operation envelope；
- `app/crud` 与 `app/eventsourced` 至少有可选接入点；
- 不启用 operation 时旧行为不破坏；
- `tracked` 协议字段存在，但允许仍是最小实现；
- 文档、示例、测试与接口一致。

