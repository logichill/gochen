package task

import (
	"context"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"gochen/clock"
	"gochen/errors"
	"gochen/logging"
	"gochen/policy/retry"
)

// TaskSupervisor 负责后台任务的生命周期与失败可观测性。
type TaskSupervisor struct {
	wg         sync.WaitGroup
	stopCh     chan struct{}
	stopOnce   sync.Once
	waitOnce   sync.Once
	stopDone   chan struct{}
	name       string
	logger     logging.ILogger
	clock      clock.IClock
	lifecycle  sync.Mutex
	isStopping bool

	mu       sync.Mutex
	failures []GoroutineFailure
}

const maxGoroutineFailures = 100

// GoroutineFailure 记录后台任务发生错误或 panic 时的诊断信息。
type GoroutineFailure struct {
	Manager string
	Task    string
	Err     error
	Panic   any
	Stack   string
	At      time.Time
}

// NewTaskSupervisor 创建一个使用默认实时时钟的任务监督器。
func NewTaskSupervisor(name string) *TaskSupervisor {
	return NewTaskSupervisorWithClock(name, nil, clock.NewRealClock())
}

// NewTaskSupervisorWithLogger 创建一个显式注入日志器的任务监督器。
func NewTaskSupervisorWithLogger(name string, logger logging.ILogger) *TaskSupervisor {
	return NewTaskSupervisorWithClock(name, logger, clock.NewRealClock())
}

// NewTaskSupervisorWithClock 创建一个可替换时钟的任务监督器。
func NewTaskSupervisorWithClock(name string, logger logging.ILogger, clk clock.IClock) *TaskSupervisor {
	if logger == nil {
		// Keep behavior safe by default: nil logger should never panic.
		// Callers are expected to inject a real component logger at composition root.
		logger = logging.NewNoopLogger()
	}
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &TaskSupervisor{
		stopCh:   make(chan struct{}),
		stopDone: make(chan struct{}),
		name:     name,
		logger:   logger,
		clock:    clk,
	}
}

// Go 启动一个受监督的 goroutine，并自动附加 panic 恢复和失败记录。
func (gm *TaskSupervisor) Go(ctx context.Context, taskName string, fn func(context.Context)) error {
	ctx, err := gm.ensureCtx(ctx, taskName)
	if err != nil {
		return err
	}
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "fn is nil").WithContext("manager", gm.name).WithContext("task", taskName)
	}
	if err := gm.beginTask(taskName); err != nil {
		return err
	}

	go func() {
		defer gm.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				gm.recordFailure(GoroutineFailure{
					Manager: gm.name,
					Task:    taskName,
					Panic:   r,
					Stack:   stack,
					At:      gm.clock.Now(),
				})
				gm.logger.Error(ctx, "goroutine panic recovered",
					logging.String("manager", gm.name),
					logging.String("task", taskName),
					logging.Any("panic", r),
					logging.String("stack", stack))
			}
		}()

		fn(ctx)
	}()
	return nil
}

// GoLoop 启动一个按固定间隔执行的受监督循环任务。
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

	go func() {
		defer gm.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				gm.recordFailure(GoroutineFailure{
					Manager: gm.name,
					Task:    taskName,
					Panic:   r,
					Stack:   stack,
					At:      gm.clock.Now(),
				})
				gm.logger.Error(ctx, "goroutine loop panic recovered",
					logging.String("manager", gm.name),
					logging.String("task", taskName),
					logging.Any("panic", r),
					logging.String("stack", stack))
			}
		}()

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
	}()
	return nil
}

// GoWithTimeout 启动一个带 best-effort 超时监督的受监督任务。
//
// 说明：
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
	if err := gm.beginTask(taskName); err != nil {
		return err
	}

	go func() {
		defer gm.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				gm.recordFailure(GoroutineFailure{
					Manager: gm.name,
					Task:    taskName,
					Panic:   r,
					Stack:   stack,
					At:      gm.clock.Now(),
				})
				gm.logger.Error(ctx, "goroutine panic recovered",
					logging.String("manager", gm.name),
					logging.String("task", taskName),
					logging.Any("panic", r),
					logging.String("stack", stack))
			}
		}()

		timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		done := make(chan error, 1)
		go func() {
			// 避免 fn() 内部 panic 导致外层永远等不到 done，从而 goroutine 泄露。
			defer func() {
				if r := recover(); r != nil {
					stack := string(debug.Stack())
					gm.recordFailure(GoroutineFailure{Manager: gm.name, Task: taskName, Panic: r, Stack: stack, At: gm.clock.Now()})
					done <- errors.NewCode(errors.Internal, "goroutine task panic").
						WithContext("manager", gm.name).
						WithContext("task", taskName).
						WithContext("panic", r)
				}
			}()
			done <- fn(timeoutCtx)
		}()

		select {
		case err := <-done:
			if err != nil {
				gm.recordFailure(GoroutineFailure{Manager: gm.name, Task: taskName, Err: err, At: gm.clock.Now()})
				gm.logger.Error(ctx, "goroutine task error",
					logging.String("manager", gm.name),
					logging.String("task", taskName),
					logging.Error(err))
			}
		case <-timeoutCtx.Done():
			timeoutErr := errors.NewCodeWithCause(errors.Timeout, "goroutine task timeout", timeoutCtx.Err()).
				WithContext("manager", gm.name).
				WithContext("task", taskName).
				WithContext("best_effort", true)
			gm.recordFailure(GoroutineFailure{Manager: gm.name, Task: taskName, Err: timeoutErr, At: gm.clock.Now()})
			gm.logger.Warn(ctx, "goroutine task timeout (best-effort)",
				logging.String("manager", gm.name),
				logging.String("task", taskName),
				logging.Duration("timeout", timeout))
		}
	}()
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
	const maxInt64 = int64(^uint64(0) >> 1)
	base := int64(backoff)
	if base <= 0 {
		return 0
	}
	shift := uint(maxRetries - 1)
	if shift >= 63 {
		return time.Duration(maxInt64)
	}
	if base > maxInt64>>shift {
		return time.Duration(maxInt64)
	}
	return time.Duration(base << shift)
}

// Run 启动一个受监督任务并阻塞等待其完成。
func (gm *TaskSupervisor) Run(ctx context.Context, taskName string, fn func(context.Context) error) (err error) {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil").
			WithContext("manager", gm.name).
			WithContext("task", taskName)
	}
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			err = errors.NewCode(errors.Internal, "goroutine task panic").
				WithContext("manager", gm.name).
				WithContext("task", taskName).
				WithContext("panic", r)
			gm.recordFailure(GoroutineFailure{Manager: gm.name, Task: taskName, Err: err, Panic: r, Stack: stack, At: gm.clock.Now()})
			gm.logger.Error(ctx, "goroutine task panic",
				logging.String("manager", gm.name),
				logging.String("task", taskName),
				logging.Any("panic", r),
				logging.String("stack", stack))
		}
	}()
	if err := fn(ctx); err != nil {
		gm.recordFailure(GoroutineFailure{Manager: gm.name, Task: taskName, Err: err, At: gm.clock.Now()})
		return err
	}
	return nil
}

func (gm *TaskSupervisor) Failures() []GoroutineFailure {
	if gm == nil {
		return nil
	}
	gm.mu.Lock()
	defer gm.mu.Unlock()
	out := make([]GoroutineFailure, len(gm.failures))
	copy(out, gm.failures)
	return out
}

// recordFailure 记录一条任务失败信息，并在达到上限后覆盖最旧记录。
func (gm *TaskSupervisor) recordFailure(f GoroutineFailure) {
	if gm == nil {
		return
	}
	gm.mu.Lock()
	if maxGoroutineFailures > 0 && len(gm.failures) >= maxGoroutineFailures {
		copy(gm.failures, gm.failures[1:])
		gm.failures[len(gm.failures)-1] = f
		gm.mu.Unlock()
		return
	}
	gm.failures = append(gm.failures, f)
	gm.mu.Unlock()
}

// ensureCtx 确保上下文。
func (gm *TaskSupervisor) ensureCtx(ctx context.Context, taskName string) (context.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil").
			WithContext("manager", gm.name).
			WithContext("task", taskName)
	}
	if strings.TrimSpace(taskName) == "" {
		return nil, errors.NewCode(errors.InvalidInput, "task name cannot be empty").
			WithContext("manager", gm.name).
			WithContext("task", taskName)
	}
	return ctx, nil
}

func (gm *TaskSupervisor) beginTask(taskName string) error {
	gm.lifecycle.Lock()
	defer gm.lifecycle.Unlock()
	if gm.isStopping {
		return errors.NewCode(errors.Conflict, "goroutine manager is stopping").
			WithContext("manager", gm.name).
			WithContext("task", taskName)
	}
	gm.wg.Add(1)
	return nil
}

// Stop 请求所有 goroutine 停止，并等待它们在 ctx 截止前完成。
func (gm *TaskSupervisor) Stop(ctx context.Context) error {
	if gm == nil {
		return nil
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil").
			WithContext("manager", gm.name).
			WithContext("task", "stop")
	}

	gm.lifecycle.Lock()
	gm.isStopping = true
	gm.stopOnce.Do(func() {
		close(gm.stopCh)
	})
	gm.lifecycle.Unlock()

	gm.waitOnce.Do(func() {
		go func() {
			gm.wg.Wait()
			close(gm.stopDone)
		}()
	})

	select {
	case <-gm.stopDone:
		return nil
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return errors.NewCode(errors.Timeout, "goroutine manager stop timeout").
				WithContext("manager", gm.name).
				WithContext("task", "stop").
				WithContext("cause", ctx.Err().Error())
		}
		return ctx.Err()
	}
}

// StopWithinParentDeadline 使用 parent 的 deadline 作为等待上限，但不继承 parent 的取消状态。
//
// 说明：
//   - 适用于 shutdown 场景：上游请求取消后仍应尽力等待后台任务收尾；
//   - parent 为 nil 时使用 context.Background()；
//   - 若 parent 的 deadline 已过期，则使用 1ms 的最小等待窗口。
func (gm *TaskSupervisor) StopWithinParentDeadline(parent context.Context, fallback time.Duration) error {
	if gm == nil {
		return nil
	}
	stopCtx, cancel := NewStopContext(parent, fallback)
	defer cancel()
	return gm.Stop(stopCtx)
}

// NewStopContext 创建不继承取消信号、但沿用 parent deadline 的停止等待 context。
func NewStopContext(parent context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	timeout := fallback
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	if deadline, ok := parent.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}
