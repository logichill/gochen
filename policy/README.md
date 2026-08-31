# policy（通用弹性与控制策略）

`gochen/policy` 属于 Core 模块，提供与具体业务无关的**调用级弹性控制策略**，可无缝嵌入在 HTTP 中间件、消息处理器、后台任务或远程 RPC 调用中。

---

## 1. 子包概览

| 子包 | 策略类型 | 核心能力 |
|---|---|---|
| **`policy/retry`** | 指数退避重试 | 支持最大尝试次数、初始与最大延迟、退避乘数、随机扰动（Jitter 防惊群）以及自定义错误判定（`RetryIf` / `IRetryableError`） |
| **`policy/ratelimit`** | 令牌桶限流 | 支持全局限流与多 Key 细粒度限流（如按用户/IP）、突发容量控制与空闲 Bucket 回收 |
| **`policy/circuit`** | 熔断器 | 经典三态状态机（Closed / Open / Half-Open），基于连续失败阈值熔断、冷却超时后试探恢复 |

---

## 2. 使用示例

### 2.1 指数退避重试 (`policy/retry`)

```go
import (
    "context"
    "time"

    "gochen/policy/retry"
)

cfg := retry.Config{
    MaxAttempts:   3,
    InitialDelay:  100 * time.Millisecond,
    BackoffFactor: 2.0,
    MaxDelay:      2 * time.Second,
    JitterRatio:   0.2, // 20% 随机抖动防惊群
}

err := retry.Do(ctx, cfg, func(execCtx context.Context) error {
    return remoteClient.Call(execCtx)
})
```

### 2.2 令牌桶限流 (`policy/ratelimit`)

```go
import "gochen/policy/ratelimit"

limiter := ratelimit.New(ratelimit.Config{
    RequestsPerSecond: 100, // 每秒 100 令牌
    BurstSize:         20,  // 允许突发 20 令牌
})

// 全局判定
if !limiter.Allow() {
    return errors.NewCode(errors.RateLimit, "too many requests")
}

// 基于业务 Key（如 IP / 用户 ID）判定
if !limiter.AllowKey(clientIP) {
    return errors.NewCode(errors.RateLimit, "rate limit exceeded for client")
}
```

### 2.3 熔断保护 (`policy/circuit`)

```go
import (
    "context"
    "time"

    "gochen/policy/circuit"
)

breaker := circuit.New(circuit.Config{
    FailureThreshold: 5,               // 连续 5 次失败触发熔断
    CoolingTimeout:   10 * time.Second, // 熔断后冷却 10 秒进入半开状态
    SuccessThreshold: 2,               // 半开状态连续 2 次成功恢复为闭合
})

err := breaker.Execute(ctx, func(execCtx context.Context) error {
    return callUnstableService(execCtx)
})
```

---

## 3. 在后台任务与中间件中的集成

- **后台监督任务**：可结合 `gochen/process/task.TaskSupervisor` 为后台常驻协程注入重试与熔断保护；
- **HTTP 服务防护**：`gochen-runtime/http/middleware` 中的 `RateLimit` 与 `CircuitBreaker` 中间件均直接基于本包构建。

