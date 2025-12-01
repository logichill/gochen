# 日志设计与注入模式（logging）

本文档描述 gochen 根模块中日志接口的设计，以及推荐的 Logger 注入模式，重点解决：

- 如何在框架与业务之间统一日志接口；
- 如何避免在库代码中滥用全局 Logger；
- 如何在需要时实现“完全静默”或细粒度注入。

## 1. 日志接口与默认实现

### 1.1 `ILogger` 接口

`logging.ILogger` 是全局统一的日志接口，所有框架组件只依赖该接口：

- 四个核心方法：
  - `Debug(ctx, msg, fields...)`
  - `Info(ctx, msg, fields...)`
  - `Warn(ctx, msg, fields...)`
  - `Error(ctx, msg, fields...)`
- `WithFields(fields...) ILogger` 用于在现有 Logger 基础上追加字段，返回新的派生 Logger。

字段使用 `logging.Field` 表达，借助辅助构造函数创建：

- `String/Int/Int64/Uint64/Float64/Bool/Any/Error/Duration` 等。

### 1.2 默认实现

- `StdLogger`：
  - 基于标准库 `log` 实现，适合作为开发环境或简单服务的默认输出；
  - 支持 prefix 与字段输出，格式约定类似：
    - `<prefix> [component] event=... msg key=value...`
- `NoopLogger`：
  - 空实现，不输出任何日志；
  - 适用于测试环境或需要完全静默的场景。

### 1.3 全局 Logger（兜底）

logging 包内部维护一个进程级的全局 Logger：

- `GetLogger() ILogger`：获取当前全局 Logger；
- `SetLogger(logger ILogger)`：设置全局 Logger；
- 初始值为 `NewStdLogger("")`。

**约定**：

- `GetLogger()` 仅作为兜底/默认使用；
- `SetLogger()` 只应在应用入口或组合根调用（例如 `main` 或 `server.Engine` 引导阶段），框架内部不应修改全局 Logger；
- 组件内部应优先使用“注入 Logger”而非反复读取全局 Logger。

## 2. 推荐的 Logger 注入模式

### 2.1 Options + Logger 字段（组件级）

对需要日志的中长生命周期组件（Outbox 发布器、ProjectionManager、CommandBus、SagaOrchestrator、EventSourcedService 等），统一采用如下模式：

1. **Options 或 Config 中包含 Logger 字段**：

   ```go
   type XXXOptions struct {
       // ...
       Logger logging.ILogger
   }
   ```

2. **组件结构体持有 logger 字段**：

   ```go
   type XXX struct {
       // ...
       logger logging.ILogger
   }
   ```

3. **构造函数中做一次性的 fallback**：

   ```go
   if opts.Logger != nil {
       x.logger = opts.Logger
   } else {
       x.logger = logging.GetLogger().WithFields(
           logging.String("component", "package.component"),
       )
   }
   ```

4. **运行期所有日志都通过字段 logger 输出**，不再在方法内部调用 `logging.GetLogger()`。

这样可以保证：

- 调用方可以按组件粒度注入 Logger 或 NoopLogger；
- 没有显式注入时仍有合理的默认日志输出；
- 全局 Logger 只作为最后兜底，而不是隐藏依赖。

### 2.2 组合根的统一注入

在应用入口或组合根（如 `main`、`server.Server`/`server.Engine` 引导阶段）建议：

1. 选定进程级根 Logger（可选）：

   ```go
   logging.SetLogger(myAppLogger)
   ```

2. 为核心组件注入派生 Logger：

   ```go
   outboxLogger := logging.GetLogger().WithFields(logging.String("component", "eventing.outbox.publisher"))
   projLogger := logging.GetLogger().WithFields(logging.String("component", "eventing.projection.manager"))
   cmdBusLogger := logging.GetLogger().WithFields(logging.String("component", "messaging.command.bus"))
   ```

3. 将这些 Logger 通过 Options/Config 注入到相应组件：

   ```go
   publisher := outbox.NewPublisher(repo, eventBus, cfg, outboxLogger)
   pm := projection.NewProjectionManagerWithConfig(eventStore, eventBus, &projection.ProjectionConfig{
       // ...
   })
   // 或在未来的 ProjectionManagerOptions 中显式传 Logger
   ```

好处是：

- 整个应用的日志风格和输出位置由组合根统一控制；
- 每个组件可以带有自己的 component 字段，方便过滤与排查；
- 测试中可以统一注入 `NewNoopLogger` 来关闭框架日志输出。

### 2.3 显式静默：NoopLogger

任何组件只要遵守“只依赖注入的 ILogger”这一条，就可以通过注入 `NewNoopLogger` 实现完全静默：

```go
noop := logging.NewNoopLogger()
publisher := outbox.NewPublisher(repo, eventBus, cfg, noop)
```

推荐在以下场景使用 NoopLogger：

- 单元测试中不希望被框架日志污染输出；
- 业务方已经在外层做了统一的监控/审计，并不关心某些内部组件的详细日志。

## 3. 典型组件中的 Logger 模式建议

### 3.1 Outbox 发布器（`eventing/outbox`）

- `NewPublisher(repo, bus, cfg, logger logging.ILogger)`：
  - 若 `logger == nil`，应 fallback 为 `logging.GetLogger().WithField("component", "eventing.outbox.publisher")`。
- `NewParallelPublisher(..., logger logging.ILogger, workerCount int)`：
  - 类似，使用 `logging.GetLogger().WithField("component", "eventing.outbox.parallel_publisher")` 作为默认。
- 内部所有日志通过 `p.log` 输出，不再直接访问全局 Logger。

### 3.2 ProjectionManager（`eventing/projection`）

- 建议增加 Options 结构（或扩展已有构造函数）：

  ```go
  type ProjectionManagerOptions struct {
      EventStore store.IEventStore
      EventBus   bus.IEventBus
      Logger     logging.ILogger
      Config     *ProjectionConfig
  }
  ```

- Manager 内部持有 `logger logging.ILogger` 字段：
  - Options.Logger 为空时的默认值：`logging.GetLogger().WithField("component", "projection.manager")`。
  - 所有投影相关日志使用该字段。

### 3.3 CommandBus（`messaging/command`）

- 在 `CommandBusConfig` 中增加 `Logger logging.ILogger`：
  - 若未注入，则在构造时 fallback 到 `logging.GetLogger().WithField("component", "messaging.command.bus")`。
- Transport 类型探测告警等日志使用 `bus.logger`，而不是直接 `logging.GetLogger()`。

### 3.4 EventSourcedService（`domain/eventsourced`）

- 当前已经通过 `EventSourcedServiceOptions.Logger` 提供 Logger 注入点：
  - 若未注入，则默认 `logging.GetLogger().WithField("component", "eventsourced.service")`。
- 建议在文档与调用示例中明确：
  - 生产环境下推荐显式注入业务 Logger；
  - 测试环境下可以传入 `NewNoopLogger()` 来关闭事件溯源服务日志。

### 3.5 SagaOrchestrator 与其他模式组件（`patterns/saga` 等）

- `patterns/saga` 等模式层组件推荐增加 Options/Config 中的 Logger 字段：
  - 默认值使用 `logging.GetLogger().WithField("component", "patterns.saga.orchestrator")`；
  - 组合根可注入统一的模式层 Logger 或 NoopLogger。

## 4. 使用约束与反例

### 4.1 推荐做法

- 仅在以下位置使用 `logging.SetLogger`：
  - 应用入口（例如 `main`）；
  - 组合根初始化逻辑（例如 `server.Engine` 引导阶段）。
- 在组件构造函数中：
  - 优先读取 Options/Config 中的 Logger；
  - 仅在 Logger 为空时通过 `logging.GetLogger().WithField("component", "...")` 等方式构造组件级默认 Logger。
- 在组件方法中：
  - 永远使用结构体的 `logger` 字段；
  - 不再散布 `logging.GetLogger()` 调用。

### 4.2 反例（应避免）

- 在任意业务函数或库函数中直接调用 `logging.GetLogger().Info(...)`，而不通过 Options 注入；
- 在库代码内部调用 `logging.SetLogger` 修改全局日志实现；
- 在同一组件内部混用“注入的 logger”与“全局 logger”，导致字段/trace 不一致。

通过上述约定，gochen 在保持 Logger 接口简单的前提下，实现了：

- 完整的注入能力（组件级、组合根级）；
- 明确的分层边界（领域/应用/模式/基础设施）；
- 对旧代码的向后兼容（未注入时继续使用全局 Logger）。
