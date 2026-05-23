package middleware

import (
	"context"
	"runtime/debug"
	"time"

	"gochen/errors"
	"gochen/httpx"
	"gochen/logging"
	"gochen/task"
)

// Timeout 为整个 handler chain 提供超时控制。
//
// 说明：
// - 该中间件通过 ctx cancellation 传递超时信号；无法强制终止不响应 ctx 的业务逻辑；
// - 为避免后台 goroutine 泄露与 stop 编排问题，不启动常驻 goroutine，仅每次请求启动一次。
func Timeout(timeout time.Duration) httpx.Middleware {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return func(ctx httpx.IContext, next func() error) error {
		if ctx == nil {
			return errors.NewCode(errors.InvalidInput, "ctx is nil")
		}
		reqCtx := ctx.RequestContext()
		if reqCtx == nil {
			return errors.NewCode(errors.InvalidInput, "request context is nil")
		}
		timeoutCtx, cancel := reqCtx.WithTimeout(timeout)
		defer cancel()

		ctx.SetContext(timeoutCtx)

		done := make(chan error, 1)
		logger := logging.ComponentLogger("gochen.http.timeout_middleware")
		super := task.NewTaskSupervisorWithLogger("gochen.http.timeout_middleware", logger)
		if err := super.Go(timeoutCtx, "next", func(ctx context.Context) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error(ctx, "Timeout middleware goroutine panic",
						logging.Any("panic", rec),
						logging.String("stack", string(debug.Stack())),
					)
					select {
					case done <- errors.NewCode(errors.Internal, "internal server error"):
					default:
					}
				}
			}()
			err := next()
			select {
			case done <- err:
			default:
			}
		}); err != nil {
			return err
		}

		const stopTimeout = 100 * time.Millisecond
		select {
		case err := <-done:
			// next() 已结束，Stop 必须是 quick path：用于等待监督 goroutine 收尾。
			stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(timeoutCtx), stopTimeout)
			defer cancelStop()
			if stopErr := super.Stop(stopCtx); stopErr != nil {
				logger.Warn(timeoutCtx, "timeout middleware stop timed out after handler completed",
					logging.String("method", ctx.Method()),
					logging.String("path", ctx.Path()),
					logging.Duration("stop_timeout", stopTimeout),
					logging.Error(stopErr),
				)
			}
			return err
		case <-timeoutCtx.Done():
			// 超时分支：Stop 必须有上界，避免“业务逻辑不响应 ctx 导致 Stop 无限等待”的可靠性问题。
			stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(timeoutCtx), stopTimeout)
			defer cancelStop()
			if err := super.Stop(stopCtx); err != nil {
				logger.Warn(timeoutCtx, "timeout middleware stop timed out (best-effort)",
					logging.String("method", ctx.Method()),
					logging.String("path", ctx.Path()),
					logging.Duration("stop_timeout", stopTimeout),
					logging.Error(err),
				)
			}
			return errors.NewCodeWithCause(errors.Timeout, "request timeout", timeoutCtx.Err())
		}
	}
}
