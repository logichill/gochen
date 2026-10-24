package task

import (
	"context"
	"strings"
	"time"

	"gochen/errors"
)

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
