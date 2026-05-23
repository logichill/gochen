package saga

import (
	"gochen/clock"
	"gochen/eventing/bus"
	"gochen/logging"
	"gochen/messaging/command"
	"gochen/process/lock"
)

// SagaOrchestrator 定义SagaOrchestrator。
type SagaOrchestrator struct {
	commandExecutor command.ICommandExecutor
	eventBus        bus.IEventBus
	stateStore      ISagaStateStore
	logger          logging.ILogger
	lock            lock.ILockProvider
	clock           clock.IClock
}

// NewSagaOrchestrator 创建SagaOrchestrator。
func NewSagaOrchestrator(
	commandExecutor command.ICommandExecutor,
	eventBus bus.IEventBus,
	stateStore ISagaStateStore,
) *SagaOrchestrator {
	o := &SagaOrchestrator{
		commandExecutor: commandExecutor,
		eventBus:        eventBus,
		stateStore:      stateStore,
		clock:           clock.NewRealClock(),
	}
	o.logger = logging.ComponentLogger("saga.orchestrator")
	return o
}

func (o *SagaOrchestrator) WithLockProvider(provider lock.ILockProvider) *SagaOrchestrator {
	if provider != nil {
		o.lock = provider
	}
	return o
}

func (o *SagaOrchestrator) WithClock(clk clock.IClock) *SagaOrchestrator {
	if o == nil {
		return o
	}
	if clk != nil {
		o.clock = clk
	}
	return o
}
