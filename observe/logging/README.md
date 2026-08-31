# logging/：日志接口与注入模式

gochen 的日志目标是：**框架代码只依赖统一接口 `logging.ILogger`**，并通过组合根/构造函数注入 logger，避免库代码运行期隐式依赖全局 logger。

## 1. 顶层概念

| 概念 | 对应类型/函数 | 作用 |
|---|---|---|
| 统一接口 | `logging.ILogger` | `Debug/Info/Warn/Error` + `WithField(s)` |
| 字段 | `logging.Field` | 结构化字段（`String/Int/Error/Any/...`） |
| 默认实现 | `logging.NewStdLogger` / `logging.NewNoopLogger` | 开箱即用 / 完全静默 |
| 组件字段 | `logging.WithComponent(logger, component)` | 给 logger 注入 `component` 字段 |
| 组件派生 | `logging.ComponentLogger(component, base)` | 从组合根提供的 base logger 派生组件字段；未注入时仅用独立 `StdLogger` 兜底 |

## 2. 推荐注入模式（强约束）

### 2.1 组件持有 logger 字段（不要在方法里反复 GetLogger）

```go
type Publisher struct {
	logger logging.ILogger
}

func NewPublisher(logger logging.ILogger) *Publisher {
	if logger == nil {
		// 建议组合根注入；这里兜底仅用于避免 nil panic。
		logger = logging.ComponentLogger("eventing.outbox.publisher")
	}
	return &Publisher{logger: logger}
}
```

这样做的收益：
- 可按组件粒度注入不同 logger（或 `NoopLogger`）
- 避免“隐藏依赖”：测试/业务不再被框架默认日志污染
- 默认行为仍可用：没注入时兜底到默认 `StdLogger`（不依赖全局单例）

### 2.2 组合根统一创建并注入 logger（推荐）

```go
// main/cmd 启动阶段创建一次，并注入到各组件。
appLogger := logging.NewStdLogger("my-app")

// 如需测试隔离或接入自定义 sink，使用实例级 writer，避免 log.SetOutput 污染全局状态。
testLogger := logging.NewLogger(logging.Config{Prefix: "test", Writer: &buf})
_ = testLogger

// 构造组件时按 component 派生（可选）
outboxLogger := logging.WithComponent(appLogger, "eventing.outbox.publisher")
_ = outboxLogger
```

约定：
- logger 由组合根创建并向下传递（构造函数参数/Options/Config）；
- 框架内部不应依赖“可变全局 logger”，避免引入隐式共享状态；
- 未显式注入 base logger 时，`ComponentLogger` 稳定回退到 `StdLogger`；框架不提供修改进程级默认 factory 的入口。

## 3. 测试中的最佳实践：NoopLogger

当你不希望测试输出被框架日志淹没时：

```go
logger := logging.NewNoopLogger()
_ = logger
```

## 4. 进一步阅读

- 接口定义：`logging/logger.go`
- 组合根与生命周期：`host/README.md`
