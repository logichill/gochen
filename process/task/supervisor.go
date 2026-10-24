package task

import (
	"context"
	"reflect"
	"runtime/debug"
	"sync"

	"gochen/clock"
	"gochen/errors"
	"gochen/observe/logging"
)

// isNilInterface 识别 nil 接口以及持有 typed-nil 具体值的接口。
func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

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
	// typed-nil 防御：接口持有 nil 具体值时 != nil 但调用方法会 panic。
	if isNilInterface(logger) {
		// Keep behavior safe by default: nil logger should never panic.
		// Callers are expected to inject a real component logger at composition root.
		logger = logging.NewNoopLogger()
	}
	if isNilInterface(clk) {
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

	gm.launchManaged(ctx, taskName, "goroutine panic recovered", func() {
		fn(ctx)
	})
	return nil
}

// Run 启动一个受监督任务并阻塞等待其完成。
func (gm *TaskSupervisor) Run(ctx context.Context, taskName string, fn func(context.Context) error) (err error) {
	ctx, err = gm.ensureCtx(ctx, taskName)
	if err != nil {
		return err
	}
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "fn is nil").WithContext("manager", gm.name).WithContext("task", taskName)
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

func (gm *TaskSupervisor) launchManaged(ctx context.Context, taskName, panicMsg string, body func()) {
	go func() {
		defer gm.wg.Done()
		defer gm.recoverPanic(ctx, taskName, panicMsg, nil)
		body()
	}()
}
