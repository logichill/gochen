package outbox

import (
	"context"
	"sync"
	"sync/atomic"

	"gochen/errors"
	"gochen/observe/logging"
	"gochen/process/task"
)

// TaskExecutor 将固定后台处理函数连接到消费方装配的受保护执行入口。
// 处理函数由发布器注册；消费方在封存前绑定执行器，并核对同一 TaskRegistry。
type TaskExecutor func(context.Context, string, func(context.Context) error) error

type publisherTasks[ID comparable] struct {
	registry *task.Registry
	repo     IOutboxRepository[ID]
	prefix   string
	mu       sync.RWMutex
	executor TaskExecutor
}

func newPublisherTasks[ID comparable](name string, repo IOutboxRepository[ID], logger logging.ILogger) (*publisherTasks[ID], error) {
	registry, err := task.NewRegistry(task.NewTaskSupervisorWithLogger(name, logger))
	if err != nil {
		return nil, err
	}
	tasks := &publisherTasks[ID]{registry: registry, repo: repo, prefix: name + ":"}
	if err := registry.Register(tasks.prefix+"renew_claim", tasks.renewClaim); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (t *publisherTasks[ID]) configure(executor TaskExecutor) error {
	if executor == nil {
		return errors.NewCode(errors.InvalidInput, "Outbox tasks require an explicit executor")
	}
	return t.registry.ConfigureHandlers(func() error {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.executor = executor
		return nil
	})
}

func (t *publisherTasks[ID]) checkActive() error {
	if t == nil {
		return errors.NewCode(errors.FailedPrecondition, "Outbox tasks have not been assembled")
	}
	t.mu.RLock()
	bound := t.executor != nil
	t.mu.RUnlock()
	if !bound {
		return errors.NewCode(errors.FailedPrecondition, "Outbox task executor has not been bound")
	}
	return t.registry.CheckActive()
}

func (t *publisherTasks[ID]) execute(ctx context.Context, name string, handler func(context.Context) error) error {
	t.mu.RLock()
	executor := t.executor
	t.mu.RUnlock()
	if executor == nil {
		return errors.NewCode(errors.FailedPrecondition, "Outbox task executor has not been bound")
	}
	var entered atomic.Bool
	var active atomic.Bool
	active.Store(true)
	defer active.Store(false)
	err := executor(ctx, t.prefix+name, func(ctx context.Context) error {
		if !active.Load() || !entered.CompareAndSwap(false, true) {
			return errors.NewCode(errors.FailedPrecondition, "registered Outbox handler can be entered only once")
		}
		return handler(ctx)
	})
	if err == nil && !entered.Load() {
		return errors.NewCode(errors.FailedPrecondition, "Outbox executor did not enter the registered handler")
	}
	return err
}

// TaskRegistry 返回串行发布器实际执行的固定后台函数注册源。
func (p *Publisher[ID]) TaskRegistry() *task.Registry { return p.tasks.registry }

// SetTaskExecutor 在入口封存前装配后台资源执行能力。
func (p *Publisher[ID]) SetTaskExecutor(executor TaskExecutor) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConfigurableLocked(); err != nil {
		return err
	}
	return p.tasks.configure(executor)
}

// TaskRegistry 返回并行发布器实际执行的固定后台函数注册源。
func (p *ParallelPublisher[ID]) TaskRegistry() *task.Registry { return p.tasks.registry }

// SetTaskExecutor 在入口封存前装配后台资源执行能力。
func (p *ParallelPublisher[ID]) SetTaskExecutor(executor TaskExecutor) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConfigurableLocked(); err != nil {
		return err
	}
	return p.tasks.configure(executor)
}
