# app/operation（写操作生命周期与可观察性运行时）

`app/operation` 为应用层写操作提供统一的**协议包装与可观察性外壳**。在实际业务中，“写操作成功”并不总是等同于“读模型已立即可见”。`app/operation` 统一回答以下问题：

- 本次写操作何时被系统接受？当前处于哪个阶段（已接受 / 处理中 / 已完成 / 失败 / 降级 / 超时）？
- 若结果处于最终一致性延迟可见状态，上层如何统一跟踪？
- 如何在写入口支持防重/幂等、状态轮询与 SSE 状态流？

> **定位**：`operation` 统一的是“**写入口返回协议与生命周期**”，而不是“消息分发机制”或“工作流编排”。复杂补偿归 `process/saga`，状态机图推进归 `process/workflow`，串行化归 `process/lock`。

---

## 1. 核心概念与执行模式

### 1.1 执行模式 (`Mode`)

| 模式 | 适用场景 | 行为特点 | 典型返回状态 |
|---|---|---|---|
| **`Direct`** | 普通同步 CRUD / 强一致写操作 | 直接执行 handler，包装统一信封返回，无额外状态跟踪 | `settled` / `failed` |
| **`Inline`** | 同步执行成功、但读模型需轻量收敛 | 同步执行 handler，并附带收敛提示与影响范围元数据 | `settled` / `processing` |
| **`Tracked`** | 长耗时/异步推进/需要前端轮询跟踪 | 生成 Operation ID，支持幂等键与状态查询/流订阅 | `accepted` $\to$ `settled` / `failed` |

### 1.2 生命周期状态 (`Status`)

- `StatusAccepted` (`"accepted"`)：写操作已被系统受理，排队或异步处理中；
- `StatusProcessing` (`"processing"`)：写操作正在执行；
- `StatusSettled` (`"settled"`)：写操作已完成，最终结果已收敛；
- `StatusFailed` (`"failed"`)：写操作执行失败；
- `StatusTimeout` (`"timeout"`)：操作超时未收敛；
- `StatusDegraded` (`"degraded"`)：操作降级完成（主流程成功但旁路降级）。

---

## 2. 核心数据结构

### 2.1 `Spec`（操作规格）

业务在调用 Runner 时显式声明操作规格：

```go
spec := &operation.Spec{
    Type:           "order.create",              // 操作类型（必填）
    Mode:           operation.ModeTracked,       // 执行模式（必填）
    Resource:       "order:1001",                // 目标资源标识
    AffectedScopes: []string{"tenant:1", "dept:2"}, // 影响的数据范围（供前端/缓存失效）
    IdempotencyKey: "req-key-12345",             // 幂等键（可选，仅 Tracked 支持）
}
```

### 2.2 `Result`（统一返回信封）

写操作统一返回 `Result` 信封：

```go
type Result struct {
    OperationID    string            `json:"operation_id"`
    Type           string            `json:"type"`
    Mode           Mode              `json:"mode"`
    Status         Status            `json:"status"`
    Resource       string            `json:"resource,omitempty"`
    Result         any               `json:"result,omitempty"`
    Error          *ErrorInfo        `json:"error,omitempty"`
    AffectedScopes []string          `json:"affected_scopes,omitempty"`
    StatusURL      string            `json:"status_url,omitempty"`
    StreamURL      string            `json:"stream_url,omitempty"`
    RetryAfterMs   int               `json:"retry_after_ms,omitempty"`
    SettlementHint *SettlementHint   `json:"settlement_hint,omitempty"`
    Metadata       map[string]string `json:"metadata,omitempty"`
}
```

---

## 3. 使用示例

### 3.1 同步写操作包装（Direct / Inline）

```go
runner := operation.DefaultRunner()

result, err := runner.Execute(ctx, &operation.Spec{
    Type:     "user.update",
    Mode:     operation.ModeDirect,
    Resource: "user:123",
}, func(execCtx context.Context) (*operation.Result, error) {
    user, err := userService.UpdateUser(execCtx, req)
    if err != nil {
        return nil, err
    }
    return &operation.Result{
        Result: user,
    }, nil
})
```

### 3.2 跟踪型写操作（Tracked + 幂等支持）

```go
store := operation.NewMemoryStore()
runner := operation.NewRunner(&operation.RunnerOptions{
    Store: store,
    StatusURLBuilder: func(id string) string {
        return "/api/v1/operations/" + id
    },
})

result, err := runner.Execute(ctx, &operation.Spec{
    Type:           "report.generate",
    Mode:           operation.ModeTracked,
    IdempotencyKey: req.Header.Get("Idempotency-Key"),
}, func(execCtx context.Context) (*operation.Result, error) {
    taskID := asyncWorker.Submit(execCtx, req)
    return &operation.Result{
        Status: operation.StatusAccepted,
        Result: map[string]any{"task_id": taskID},
    }, nil
})
```

---

## 4. 相关规范

- 状态机迁移、幂等语义、JSON 契约与硬性约束见 [`SPEC.md`](SPEC.md)。
