package task

import (
	"context"
	"runtime/debug"
	"time"

	"gochen/observe/logging"
)

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

type supervisedTaskPanicError struct {
	err error
}

func (e *supervisedTaskPanicError) Error() string { return e.err.Error() }

func (e *supervisedTaskPanicError) Unwrap() error { return e.err }

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

func (gm *TaskSupervisor) recoverPanic(ctx context.Context, taskName, msg string, onRecover func(r any, stack string)) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())
		gm.recordFailure(GoroutineFailure{
			Manager: gm.name,
			Task:    taskName,
			Panic:   r,
			Stack:   stack,
			At:      gm.clock.Now(),
		})
		gm.logger.Error(ctx, msg,
			logging.String("manager", gm.name),
			logging.String("task", taskName),
			logging.Any("panic", r),
			logging.String("stack", stack))
		if onRecover != nil {
			onRecover(r, stack)
		}
	}
}
