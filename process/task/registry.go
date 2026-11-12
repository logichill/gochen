package task

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"gochen/errors"
)

// Registry 保存实际任务处理函数。登记、封存、启用分别发生在装配和核对阶段。
// 动态实例通过 context 携带输入，持续调用同一已登记处理函数。
type Registry struct {
	mu         sync.RWMutex
	supervisor *TaskSupervisor
	handlers   map[string]func(context.Context) error
	sealed     bool
	active     bool
	stopped    bool
}

// NewRegistry 将处理函数注册源连接到监督器。
func NewRegistry(supervisor *TaskSupervisor) (*Registry, error) {
	if supervisor == nil {
		return nil, errors.NewCode(errors.InvalidInput, "task registry requires a supervisor")
	}
	return &Registry{supervisor: supervisor, handlers: make(map[string]func(context.Context) error)}, nil
}

// Register 固定逻辑任务与实际函数的对应关系，拒绝覆盖及封存后新增。
func (r *Registry) Register(name string, handler func(context.Context) error) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(name) != name || handler == nil {
		return errors.NewCode(errors.InvalidInput, "task requires a stable name and handler")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return errors.NewCode(errors.FailedPrecondition, "task handlers are sealed")
	}
	if _, exists := r.handlers[name]; exists {
		return errors.NewCode(errors.Conflict, "task handler is already registered").WithContext("task", name)
	}
	r.handlers[name] = handler
	return nil
}

// RegisteredHandlers 返回稳定逻辑任务名的独立有序副本。
func (r *Registry) RegisteredHandlers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ConfigureHandlers 将处理函数关联的执行装配与封存原子串行化。
// configure 在注册表锁内运行，须仅修改关联装配并避免再次访问注册表。
func (r *Registry) ConfigureHandlers(configure func() error) error {
	if configure == nil {
		return errors.NewCode(errors.InvalidInput, "handler configuration is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return errors.NewCode(errors.FailedPrecondition, "task handlers are sealed")
	}
	return configure()
}

// CheckActive 在启动多个关联任务之前检查统一启用状态。
func (r *Registry) CheckActive() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.active {
		return errors.NewCode(errors.FailedPrecondition, "task handlers have not passed entry validation")
	}
	return nil
}

// SealHandlers 固定后续执行所使用的全部处理函数。
func (r *Registry) SealHandlers() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sealed = true
	return nil
}

// ActivateHandlers 由组合根在全部入口核对成功后调用。
func (r *Registry) ActivateHandlers() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.sealed {
		return errors.NewCode(errors.FailedPrecondition, "task handlers must be sealed before activation")
	}
	if r.stopped {
		select {
		case <-r.supervisor.stopDone:
			r.supervisor = NewTaskSupervisorWithClock(r.supervisor.name, r.supervisor.logger, r.supervisor.clock)
			r.stopped = false
		default:
			return errors.NewCode(errors.Conflict, "task registry has pending cleanup")
		}
	}
	r.active = true
	return nil
}

func (r *Registry) handler(name string) (*TaskSupervisor, func(context.Context) error, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.active {
		return nil, nil, errors.NewCode(errors.FailedPrecondition, "task handlers have not passed entry validation")
	}
	handler, exists := r.handlers[name]
	if !exists {
		return nil, nil, errors.NewCode(errors.NotFound, "task handler is not registered").WithContext("task", name)
	}
	return r.supervisor, handler, nil
}

// Run 同步执行已核对的处理函数。
func (r *Registry) Run(ctx context.Context, name string) error {
	supervisor, handler, err := r.handler(name)
	if err != nil {
		return err
	}
	if err := supervisor.beginTask(name); err != nil {
		return err
	}
	defer supervisor.wg.Done()
	return supervisor.Run(ctx, name, handler)
}

// Go 异步执行已核对的处理函数，失败沿用监督器记录。
func (r *Registry) Go(ctx context.Context, name string) error {
	supervisor, handler, err := r.handler(name)
	if err != nil {
		return err
	}
	return supervisor.Go(ctx, name, func(ctx context.Context) {
		// Run 已将处理失败记录到同一监督器。
		_ = supervisor.Run(ctx, name, handler)
	})
}

// GoLoop 立即执行一次，再按固定周期执行同一已核对的处理函数。
func (r *Registry) GoLoop(ctx context.Context, name string, interval time.Duration) error {
	supervisor, handler, err := r.handler(name)
	if err != nil {
		return err
	}
	return supervisor.GoLoop(ctx, name, interval, handler)
}

// Stop 阻止新任务进入，并等待本次执行资源释放；失败后可重试停止。
func (r *Registry) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "task stop context is required")
	}
	r.mu.Lock()
	r.stopped, r.active = true, false
	supervisor := r.supervisor
	r.mu.Unlock()
	return supervisor.Stop(ctx)
}
