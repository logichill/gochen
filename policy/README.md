# policy：重试、限流与熔断

`gochen/policy` 提供调用级控制策略，可用于 HTTP、消息处理、后台任务和外部服务调用。策略实例由组合根创建和注入。

## 接口与配置

| 子包 | 使用入口 | 关键配置 |
| --- | --- | --- |
| `policy/retry` | `retry.Do(ctx, op, cfg)` / `DoWithInfo(ctx, op, cfg)` | MaxAttempts、InitialDelay、BackoffFactor、MaxDelay、JitterRatio、RetryIf |
| `policy/ratelimit` | `ratelimit.New(cfg).Allow(key)` | RequestsPerSecond、BurstSize、WindowSize |
| `policy/circuit` | `circuit.New(cfg).Call(fn)` | MaxFailures、ResetTimeout；可选 WindowSize、FailureRateThreshold、MinimumRequests |

三个 Config 都支持注入 `clock.IClock`，便于确定性测试。

## 重试

`retry.Operation` 为 `func(context.Context) error`，配置是 `Do` 的第三个参数。MaxAttempts 包含首次执行。`DefaultConfig()` 为两次尝试、2ms 初始延迟、2 倍退避、1s 最大延迟，默认不加 jitter。

`RetryIf` 可定制错误判断；默认不重试 context 取消 / 超时与明确不可恢复的框架业务错误。重试可能再次调用业务逻辑，操作须满足幂等或可重入要求。

## 限流

`Limiter.Allow(key string)` 使用 key 区分令牌桶。同一固定 key 可作为全局配额，用户 / IP 等 key 可用于细分配额。RequestsPerSecond 为 `float64`，支持小数速率，例如 `0.5` 表示每分钟补充 30 个令牌；有限非正值不限流，NaN / Inf 拒绝请求。BurstSize 非正时使用速率向上取整值，最少为 1。

`Limiter.Tokens(key)` 读取当前令牌数，不消耗令牌，也不延长闲置时间。WindowSize 控制闲置清理频率；只回收已补满的空闲桶，避免低速率配额在清理后被提前重置。

复用同一 limiter 实例才能累计配额，不应为每次请求创建实例。

## 熔断

`Breaker.Call(func() error)` 在 closed / open / half-open 三态间转换。默认按连续失败次数触发；WindowSize 大于零时按滑动窗口失败率判断，MinimumRequests 控制最少样本数。

open 时拒绝调用，ResetTimeout 后允许半开试探。回调需要的 context 由业务闭包传递。复用 breaker 实例以保留失败统计。

## 示例与集成

在 Core 仓库执行：

```bash
GOWORK=off go run ./examples/task/policy
```

源码见[任务与策略示例](../examples/task/policy/main.go)。后台生命周期由 `process/task.TaskSupervisor` 管理；Runtime `http/middleware` 的限流与熔断中间件复用这些策略。
