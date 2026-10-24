package task

import (
	"context"
	"math"
	"time"

	"gochen/errors"
	"gochen/observe/logging"
	"gochen/policy/retry"
)

// GoLoop 启动一个按固定间隔执行的受监督循环任务。
//
// 说明：
// - fn 失败只记录并继续，调度间隔固定，不提供 backoff；
// - 需要退避/重试策略时请使用 GoWithRetry，或在 fn 内自行控制；
// - interval 必须为正数；<= 0 返回 InvalidInput。
func (gm *TaskSupervisor) GoLoop(ctx context.Context, taskName string, interval time.Duration, fn func(context.Context) error) error {
	ctx, err := gm.ensureCtx(ctx, taskName)
	if err != nil {
		return err
	}
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "fn is nil").WithContext("manager", gm.name).WithContext("task", taskName)
	}
	if interval <= 0 {
		return errors.NewCode(errors.InvalidInput, "interval must be positive").WithContext("manager", gm.name).WithContext("task", taskName)
	}
	if err := gm.beginTask(taskName); err != nil {
		return err
	}

	gm.launchManaged(ctx, taskName, "goroutine loop panic recovered", func() {
		ticker, err := gm.clock.NewTicker(interval)
		if err != nil {
			gm.recordFailure(GoroutineFailure{Manager: gm.name, Task: taskName, Err: err, At: gm.clock.Now()})
			gm.logger.Error(ctx, "goroutine loop ticker init failed",
				logging.String("manager", gm.name),
				logging.String("task", taskName),
				logging.Error(err))
			return
		}
		defer ticker.Stop()

		// 立即执行一次
		if err := fn(ctx); err != nil {
			gm.recordFailure(GoroutineFailure{Manager: gm.name, Task: taskName, Err: err, At: gm.clock.Now()})
			gm.logger.Error(ctx, "goroutine loop error",
				logging.String("manager", gm.name),
				logging.String("task", taskName),
				logging.Error(err))
		}

		for {
			select {
			case <-ticker.C():
				if err := fn(ctx); err != nil {
					gm.recordFailure(GoroutineFailure{Manager: gm.name, Task: taskName, Err: err, At: gm.clock.Now()})
					gm.logger.Error(ctx, "goroutine loop error",
						logging.String("manager", gm.name),
						logging.String("task", taskName),
						logging.Error(err))
				}
			case <-gm.stopCh:
				gm.logger.Info(ctx, "goroutine loop stopped",
					logging.String("manager", gm.name),
					logging.String("task", taskName))
				return
			case <-ctx.Done():
				gm.logger.Info(ctx, "goroutine loop cancelled",
					logging.String("manager", gm.name),
					logging.String("task", taskName),
					logging.Error(ctx.Err()))
				return
			}
		}
	})
	return nil
}

// GoWithTimeout 启动一个带 best-effort 超时监督的受监督任务。
//
// 说明：
// - timeout 必须为正数；<= 0 返回 InvalidInput；
// - timeout 只通过 context cancellation 通知 fn，并在超时后记录失败；
// - 如果 fn 不响应 ctx，监督 goroutine 会按超时结束，但 fn 所在 goroutine 仍可能继续运行；
// - 调用方必须让 fn 主动监听 ctx，不能依赖该方法强制终止阻塞调用。
func (gm *TaskSupervisor) GoWithTimeout(ctx context.Context, taskName string, timeout time.Duration, fn func(context.Context) error) error {
	ctx, err := gm.ensureCtx(ctx, taskName)
	if err != nil {
		return err
	}
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "fn is nil").WithContext("manager", gm.name).WithContext("task", taskName)
	}
	if timeout <= 0 {
		return errors.NewCode(errors.InvalidInput, "timeout must be positive").WithContext("manager", gm.name).WithContext("task", taskName)
	}
	if err := gm.beginTask(taskName); err != nil {
		return err
	}

	gm.launchManaged(ctx, taskName, "goroutine panic recovered", func() {
		timeoutCtx, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)

		timer := gm.clock.NewTimer(timeout)
		defer timer.Stop()

		done := make(chan error, 1)
		go func() {
			// 避免 fn() 内部 panic 导致外层永远等不到 done，从而 goroutine 泄露。
			defer gm.recoverPanic(ctx, taskName, "goroutine task panic", func(r any, _ string) {
				done <- &supervisedTaskPanicError{err: errors.NewCode(errors.Internal, "goroutine task panic").
					WithContext("manager", gm.name).
					WithContext("task", taskName).
					WithContext("panic", r)}
			})
			done <- fn(timeoutCtx)
		}()

		recordTimeout := func(cause error) {
			if cause == nil {
				cause = context.DeadlineExceeded
			}
			timeoutErr := errors.NewCodeWithCause(errors.Timeout, "goroutine task timeout", cause).
				WithContext("manager", gm.name).
				WithContext("task", taskName).
				WithContext("best_effort", true)
			gm.recordFailure(GoroutineFailure{Manager: gm.name, Task: taskName, Err: timeoutErr, At: gm.clock.Now()})
			gm.logger.Warn(ctx, "goroutine task timeout (best-effort)",
				logging.String("manager", gm.name),
				logging.String("task", taskName),
				logging.Duration("timeout", timeout))
		}

		select {
		case err := <-done:
			if err != nil {
				var panicErr *supervisedTaskPanicError
				if errors.As(err, &panicErr) {
					return
				}
				gm.recordFailure(GoroutineFailure{Manager: gm.name, Task: taskName, Err: err, At: gm.clock.Now()})
				gm.logger.Error(ctx, "goroutine task error",
					logging.String("manager", gm.name),
					logging.String("task", taskName),
					logging.Error(err))
			}
		case <-timer.C():
			cancel(context.DeadlineExceeded)
			recordTimeout(context.DeadlineExceeded)
		case <-ctx.Done():
			cancel(ctx.Err())
			recordTimeout(ctx.Err())
		}
	})
	return nil
}

// GoWithRetry 启动一个带重试策略的受监督任务。
func (gm *TaskSupervisor) GoWithRetry(ctx context.Context, taskName string, maxRetries int, backoff time.Duration, fn func(context.Context) error) error {
	if _, err := gm.ensureCtx(ctx, taskName); err != nil {
		return err
	}
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "fn is nil").WithContext("manager", gm.name).WithContext("task", taskName)
	}
	return gm.Go(ctx, taskName, func(ctx context.Context) {
		if maxRetries < 0 {
			maxRetries = 0
		}
		if backoff < 0 {
			backoff = 0
		}

		// 将 stop 信号合并到 ctx：确保 retry sleep 能及时退出。
		stopCtx, cancel := context.WithCancel(ctx)
		go func() {
			select {
			case <-gm.stopCh:
				cancel()
			case <-stopCtx.Done():
			}
		}()
		defer cancel()

		// 通过 policy/retry 复用成熟的 timer drain/ctx cancellation 语义。
		// 说明：
		// - attempt 是 1-based；
		// - 当前策略对任意 err 都允许重试（直到 MaxAttempts 耗尽）。
		cfg := retry.Config{
			MaxAttempts:   1 + maxRetries,
			InitialDelay:  backoff,
			BackoffFactor: 2.0,
			MaxDelay:      maxDelayForRetries(backoff, maxRetries),
			JitterRatio:   0,
			Clock:         gm.clock,
			RetryIf: func(err error) bool {
				_ = err
				return true
			},
		}

		err := retry.DoWithInfo(stopCtx, func(opCtx context.Context, attempt int) error {
			callErr := fn(opCtx)
			if callErr == nil {
				return nil
			}
			if attempt < cfg.MaxAttempts {
				delay := retry.ComputeDelay(cfg, attempt)
				gm.logger.Warn(opCtx, "goroutine retry",
					logging.String("manager", gm.name),
					logging.String("task", taskName),
					logging.Int("attempt", attempt),
					logging.Int("max_retries", maxRetries),
					logging.Duration("backoff", delay),
					logging.Error(callErr))
			}
			return callErr
		}, cfg)
		if err == nil {
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}

		gm.recordFailure(GoroutineFailure{Manager: gm.name, Task: taskName, Err: err, At: gm.clock.Now()})
		gm.logger.Error(ctx, "goroutine failed after retries",
			logging.String("manager", gm.name),
			logging.String("task", taskName),
			logging.Int("max_retries", maxRetries),
			logging.Error(err))
	})
}

// maxDelayForRetries 计算重试配置中实际出现的最大等待时间。
func maxDelayForRetries(backoff time.Duration, maxRetries int) time.Duration {
	if backoff <= 0 {
		return 0
	}
	if maxRetries <= 0 {
		return backoff
	}

	// 最大 delay 出现在最后一次 sleep：backoff * 2^(maxRetries-1)
	base := int64(backoff)
	if base <= 0 {
		return 0
	}
	shift := uint(maxRetries - 1)
	if shift >= 63 {
		return time.Duration(math.MaxInt64)
	}
	if base > math.MaxInt64>>shift {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(base << shift)
}
