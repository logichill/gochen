package saga

import (
	"context"
	"reflect"
	"time"

	"gochen/clock"
	"gochen/errors"
	"gochen/eventing/bus"
	"gochen/gen"
	"gochen/messaging/command"
	"gochen/observe/logging"
	"gochen/process/lock"
	"gochen/process/task"
)

// DefaultSagaCompensationTimeout 补偿执行的默认超时上限。
// 补偿上下文与父 ctx 解绑后必须自带期限，防止回滚无限期执行使 saga
// 永久停留在 compensating。
const DefaultSagaCompensationTimeout = 5 * time.Minute

// SagaOrchestrator 定义SagaOrchestrator。
type SagaOrchestrator struct {
	commandExecutor  command.ICommandExecutor
	eventBus         bus.IEventBus
	stateStore       ISagaStateStore
	logger           logging.ILogger
	lock             lock.ILockProvider
	clock            clock.IClock
	eventIDGenerator gen.IGenerator[string]
	supervisor       *task.TaskSupervisor
	// compensationTimeout 补偿执行的专用超时上限；非正值时回退到默认值。
	compensationTimeout time.Duration
}

// NewSagaOrchestrator 创建SagaOrchestrator。
func NewSagaOrchestrator(
	commandExecutor command.ICommandExecutor,
	eventBus bus.IEventBus,
	stateStore ISagaStateStore,
	eventIDGenerator gen.IGenerator[string],
	logger logging.ILogger,
) (*SagaOrchestrator, error) {
	if isNilSagaDependency(eventIDGenerator) {
		return nil, errors.NewCode(errors.InvalidInput, "saga event ID generator is required")
	}
	if isNilSagaDependency(logger) {
		return nil, errors.NewCode(errors.InvalidInput, "saga logger is required")
	}
	if isNilSagaDependency(commandExecutor) {
		commandExecutor = nil
	}
	if isNilSagaDependency(eventBus) {
		eventBus = nil
	}
	if isNilSagaDependency(stateStore) {
		stateStore = nil
	}
	o := &SagaOrchestrator{
		commandExecutor:     commandExecutor,
		eventBus:            eventBus,
		stateStore:          stateStore,
		clock:               clock.NewRealClock(),
		eventIDGenerator:    eventIDGenerator,
		logger:              logging.WithComponent(logger, "saga.orchestrator"),
		compensationTimeout: DefaultSagaCompensationTimeout,
	}
	o.supervisor = task.NewTaskSupervisorWithLogger("process.saga.orchestrator", o.logger)
	return o, nil
}

func isNilSagaDependency(value any) bool {
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

// WithLockProvider 设置分布式锁提供器，用于 Saga 执行期间的互斥保护。
func (o *SagaOrchestrator) WithLockProvider(provider lock.ILockProvider) *SagaOrchestrator {
	if o == nil {
		return o
	}
	if !isNilSagaDependency(provider) {
		o.lock = provider
	}
	return o
}

// WithCompensationTimeout 设置补偿执行的专用超时上限。
// 仅接受正值；非正值忽略并保留当前值。补偿上下文与父 ctx 解绑后，
// 该上限是补偿无限期执行的唯一兜底。
func (o *SagaOrchestrator) WithCompensationTimeout(timeout time.Duration) *SagaOrchestrator {
	if o == nil || timeout <= 0 {
		return o
	}
	o.compensationTimeout = timeout
	return o
}

// effectiveCompensationTimeout 返回补偿执行的超时上限。
func (o *SagaOrchestrator) effectiveCompensationTimeout() time.Duration {
	if o.compensationTimeout > 0 {
		return o.compensationTimeout
	}
	return DefaultSagaCompensationTimeout
}

// WithClock 注入 Saga 使用的时钟（测试或需要可控时间的场景）。
func (o *SagaOrchestrator) WithClock(clk clock.IClock) *SagaOrchestrator {
	if o == nil {
		return o
	}
	if !isNilSagaDependency(clk) {
		o.clock = clk
	}
	return o
}

// updateState 更新状态。
func (o *SagaOrchestrator) updateState(ctx context.Context, state *SagaState) error {
	if o.stateStore == nil {
		return nil
	}

	if err := o.stateStore.Update(ctx, state); err != nil {
		o.logger.Error(ctx, "failed to update saga state",
			logging.String("saga_id", state.SagaID),
			logging.String("status", string(state.Status)),
			logging.Error(err))
		return errors.NewCodeWithCause(errors.Database, "failed to update saga state", err).WithContext("saga_id", state.SagaID)
	}
	return nil
}
