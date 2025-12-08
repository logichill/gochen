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
   outboxLogger := logging.ComponentLogger("eventing.outbox.publisher")
   projLogger := logging.ComponentLogger("eventing.projection.manager")
   cmdBusLogger := logging.ComponentLogger("messaging.command.bus")
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
      EventStore store.IEventStore[int64]
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

- 当前已经通过 `domain/eventsourced.EventSourcedServiceOptions.Logger` 提供 Logger 注入点：
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

## 5. 日志级别与字段规范

### 5.1 日志级别选择

- `Debug`：
  - 仅用于调试与本地开发场景；
  - 典型内容：重建聚合时的事件遍历、投影重放进度、上下文注入等高频但非关键信息。
- `Info`：
  - 用于记录正常但重要的业务/系统事件；
  - 典型内容：组件启动/关闭、快照恢复命中、Outbox 发布批次完成、关键配置生效等。
- `Warn`：
  - 表示操作发生异常但可以自动降级或回退，当前请求不一定失败；
  - 典型内容：快照加载失败后回退到全量重放、未知 Transport 类型导致的保守告警、非致命的配置不一致等；
  - 约定：Warn 日志应携带足够上下文（如 aggregate_id / command_type / saga_id），便于后续排查。
- `Error`：
  - 表示当前操作失败且对调用方可见，通常伴随错误返回值；
  - 典型内容：Outbox 写入失败、事件升级过程中 payload 损坏、命令处理器返回不可恢复错误等；
  - 约定：Error 日志应尽量与返回的错误对象一致，并附带 `error_code`（若存在）与关键 ID。

### 5.2 字段命名规范

- 核心维度字段：
  - `component`：组件名，统一采用“包名.组件名”形式，例如 `eventing.outbox.publisher`、`messaging.command.bus`；
  - `event`：可选事件名，用于标识当前日志对应的内部事件，例如 `event="append_failed"`、`event="snapshot_load_failed"`。
- 常用业务字段（推荐）：
  - `aggregate_type` / `aggregate_id`：事件溯源与投影相关日志；
  - `command_type` / `command_id`：命令总线与 Saga/Workflow 相关日志；
  - `saga_id` / `step_name`：Saga 编排相关日志；
  - `error_code`：当错误已被规范化为 `errors.AppError` 或模块错误类型时，附加对应错误码；
  - `retry_count` / `next_retry_at`：Outbox 与重试相关日志。
- 其他字段：
  - 优先使用小写下划线风格的 key（如 `snapshot_version`、`cursor_after`），避免在同一模块内混用多种命名方式；
  - 对于 time.Duration 类型，优先使用 `Duration` 辅助函数创建字段（例如 `logging.Duration("elapsed", elapsed)`）。

### 5.3 关键路径约定

- Outbox 与事件存储：
  - 关键错误（写入失败/反序列化失败/移动到 DLQ 失败等）一律使用 `Error` 级别，并附带 `aggregate_type`、`aggregate_id`、`error_code` 等字段；
  - 非致命降级（例如扫描游标缺失而回退全量扫描）使用 `Warn` 级别，并在 message 中明确说明降级行为。
- 投影与命令总线：
  - 投影恢复/重放失败使用 `Error` 级别，并记录 projection 名称与事件范围；
  - CommandBus 在无法探测 Transport 类型或探测到非预期实现时，应记录一次性 `Warn` 日志，带上 `transport_type` 字段，帮助排查错误语义问题。

通过统一日志级别与字段命名，调用方可以更容易地在日志/监控系统中集中检索"关键错误路径"，并将框架日志与业务日志统一纳入观测体系。

## 6. 文档与示例中的错误处理

### 6.1 Demo 代码的快速失败模式

在 `examples/*` 和文档中的代码片段，推荐使用 `must(err)` 或 `log.Fatal(err)` 来简化错误处理：

```go
// 示例代码推荐写法
agg, err := repo.GetByID(ctx, id)
must(err)  // 失败时立即终止程序

func must(err error) {
    if err != nil {
        log.Fatal(err)
    }
}
```

**为什么在 demo 中使用 `must(err)`**：
- 让读者专注核心流程，避免错误处理细节淹没主线逻辑
- 明确表达"这里失败就不应该继续"，比用 `_` 掩盖错误更诚实
- 避免初学者误以为"忽略错误是合理做法"

### 6.2 生产代码的错误返回模式

**生产代码不应使用 `must` 或 `panic`**，而是应该返回错误，让调用方决定处理策略：

```go
// 生产代码推荐写法
func (s *Service) DoSomething(ctx context.Context, id int64) error {
    agg, err := s.repo.GetByID(ctx, id)
    if err != nil {
        return fmt.Errorf("get aggregate %d: %w", id, err)
    }
    
    // ... 业务逻辑
    
    if err := s.repo.Save(ctx, agg); err != nil {
        // 记录结构化日志
        s.logger.Error(ctx, "failed to save aggregate",
            logging.Int64("aggregate_id", id),
            logging.Error(err),
        )
        return fmt.Errorf("save aggregate %d: %w", id, err)
    }
    
    return nil
}
```

**生产代码的错误处理策略**：
1. **在业务边界统一处理**：通过 `errors.Normalize` 将领域/仓储/事件错误转换为 `AppError`
2. **根据错误码决定策略**：
   - 版本冲突（`VERSION_CONFLICT`）：重试或拒绝
   - 实体不存在（`ENTITY_NOT_FOUND`）：返回 404 或触发降级逻辑
   - 存储失败：记录 Error 级别日志并上报监控
3. **记录结构化日志**：Error 级别日志应包含 `error_code`、`aggregate_id` 等关键字段

### 6.3 特殊场景的显式标注

若业务逻辑中存在"预期失败"的场景（如 Saga 补偿、幂等检测），必须用注释明确说明：

```go
// 预期失败场景：Credit 在 false 参数下会故意失败，触发 Saga 补偿逻辑
if err := orch.Credit(ctx, id, false); err != nil {
    log.Printf("Credit failed as expected (triggering compensation): %v", err)
}
```

**关键点**：
- 不要用裸的 `_` 掩盖错误，即使在 demo 中也要让意图清晰可见
- 通过注释说明"为什么这里不 panic"，让读者理解这是业务设计而非编码疏忽
