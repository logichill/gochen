# logging：日志与注入

`gochen/observe/logging` 提供统一 ILogger 和结构化字段。组合根创建 logger，经构造函数、Config 或 options 注入组件。

## 入口

| 入口 | 用途 |
| --- | --- |
| `ILogger` | Debug / Info / Warn / Error 与 WithField(s) |
| `Field`、`String`、`Int`、`Error`、`Any` | 结构化日志字段 |
| `NewStdLogger(prefix)` | 标准文本日志 |
| `NewLogger(Config)` | 配置日志实例与 writer |
| `NewNoopLogger()` | 静默实现 |
| `WithComponent(logger, name)` | 派生组件字段 |
| `ComponentLogger(name, base...)` | 从注入的 base 派生；未传 base 时使用独立 StdLogger |

接口见 [logger.go](logger.go)。实例级 writer 可用于测试隔离，避免修改进程全局 log 输出。

## 组件用法

```go
package example

import (
	"context"

	"gochen/observe/logging"
)

type Worker struct {
	logger logging.ILogger
}

func NewWorker(base logging.ILogger) *Worker {
	return &Worker{logger: logging.ComponentLogger("worker", base)}
}

func (w *Worker) Report(ctx context.Context, taskID string) {
	w.logger.Info(ctx, "task completed", logging.String("task_id", taskID))
}
```

组件持有 logger 实例，在方法中复用。测试可注入 NoopLogger，日志 sink 由组合根选择，库代码不依赖可变全局 logger。

Host 装配见 `gochen-runtime` 仓库 `host/README.md`；错误与调用栈见 [errors](../../errors/README.md)。
