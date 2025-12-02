package saga

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/messaging"
	"gochen/messaging/command"
	synctransport "gochen/messaging/transport/sync"
)

type mockSagaEventBus struct {
	events []eventing.IEvent
}

func (m *mockSagaEventBus) PublishEvent(ctx context.Context, evt eventing.IEvent) error {
	m.events = append(m.events, evt)
	return nil
}

func (m *mockSagaEventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	for _, evt := range events {
		_ = m.PublishEvent(ctx, evt)
	}
	return nil
}

// Unused in tests
func (m *mockSagaEventBus) Publish(ctx context.Context, message messaging.IMessage) error { return nil }
func (m *mockSagaEventBus) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	return nil
}
func (m *mockSagaEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) error {
	return nil
}
func (m *mockSagaEventBus) UnsubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) error {
	return nil
}
func (m *mockSagaEventBus) SubscribeHandler(ctx context.Context, handler bus.IEventHandler) error {
	return nil
}
func (m *mockSagaEventBus) UnsubscribeHandler(ctx context.Context, handler bus.IEventHandler) error {
	return nil
}
func (m *mockSagaEventBus) Subscribe(ctx context.Context, messageType string, handler messaging.IMessageHandler) error {
	return nil
}
func (m *mockSagaEventBus) Unsubscribe(ctx context.Context, messageType string, handler messaging.IMessageHandler) error {
	return nil
}
func (m *mockSagaEventBus) Use(middleware messaging.IMiddleware) {}

var _ bus.IEventBus = (*mockSagaEventBus)(nil)

// simpleSaga 用于测试的最小 Saga
type simpleSaga struct{}

func (s *simpleSaga) GetID() string { return "saga-1" }
func (s *simpleSaga) GetSteps() []*SagaStep {
	return []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd1", "TestCommand", 1, "Test", nil), nil
		}),
	}
}
func (s *simpleSaga) OnComplete(ctx context.Context) error { return nil }
func (s *simpleSaga) OnFailed(ctx context.Context, err error) error {
	return nil
}

func TestSagaOrchestrator_PublishEvents(t *testing.T) {
	ctx := context.Background()

	// CommandBus 使用同步传输，保证立即执行
	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(ctx))
	defer transport.Close()

	msgBus := messaging.NewMessageBus(transport)
	cmdBus := command.NewCommandBus(msgBus, nil)

	require.NoError(t, cmdBus.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	mockBus := &mockSagaEventBus{}

	orchestrator := NewSagaOrchestrator(cmdBus, mockBus, nil)
	err := orchestrator.Execute(ctx, &simpleSaga{})
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(mockBus.events), 3) // Started + StepCompleted + Completed 至少 3 个
	for _, evt := range mockBus.events {
		require.Equal(t, "saga-1", evt.GetMetadata()["saga_id"])
	}
}

// failingSaga 用于测试失败 + 补偿语义
type failingSaga struct {
	steps           []*SagaStep
	failedCalled    bool
	completedCalled bool
}

func (s *failingSaga) GetID() string { return "saga-fail-1" }

func (s *failingSaga) GetSteps() []*SagaStep {
	return s.steps
}

func (s *failingSaga) OnComplete(ctx context.Context) error {
	s.completedCalled = true
	return nil
}

func (s *failingSaga) OnFailed(ctx context.Context, err error) error {
	s.failedCalled = true
	return nil
}

// 当某个步骤失败且补偿成功时，应执行补偿、调用 OnFailed，并发布相应 Saga 事件。
func TestSagaOrchestrator_StepFailure_WithSuccessfulCompensation(t *testing.T) {
	ctx := context.Background()

	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(ctx))
	defer transport.Close()

	msgBus := messaging.NewMessageBus(transport)
	cmdBus := command.NewCommandBus(msgBus, nil)

	var step1Executed, step1Compensated, step2Executed int

	require.NoError(t, cmdBus.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		step1Executed++
		return nil
	}))
	require.NoError(t, cmdBus.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		step1Compensated++
		return nil
	}))
	require.NoError(t, cmdBus.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		step2Executed++
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}

	saga := &failingSaga{}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1", "CmdStep1", 1, "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1-comp", "CmdStep1Comp", 1, "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step2", "CmdStep2", 1, "Step2", nil), nil
		}),
	}

	orchestrator := NewSagaOrchestrator(cmdBus, mockBus, stateStore)
	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSagaStepFailed)

	// 步骤执行与补偿情况
	assert.Equal(t, 1, step1Executed)
	assert.Equal(t, 1, step2Executed)
	assert.Equal(t, 1, step1Compensated)

	// OnFailed 应被调用，而 OnComplete 不应被调用
	assert.True(t, saga.failedCalled)
	assert.False(t, saga.completedCalled)

	// 状态应被标记为已补偿
	state, stateErr := stateStore.Load(ctx, saga.GetID())
	require.NoError(t, stateErr)
	assert.True(t, state.IsCompensated())

	// 事件总线中应包含步骤失败与补偿完成事件
	var hasStepFailed, hasCompCompleted bool
	for _, evt := range mockBus.events {
		switch evt.GetType() {
		case EventSagaStepFailed:
			hasStepFailed = true
		case EventSagaCompensationCompleted:
			hasCompCompleted = true
		}
	}
	assert.True(t, hasStepFailed)
	assert.True(t, hasCompCompleted)
}

// 当补偿本身失败时，应发布 SagaFailed 事件，并通过 OnFailed 暴露错误。
func TestSagaOrchestrator_CompensationFailure_EmitsSagaFailed(t *testing.T) {
	ctx := context.Background()

	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(ctx))
	defer transport.Close()

	msgBus := messaging.NewMessageBus(transport)
	cmdBus := command.NewCommandBus(msgBus, nil)

	var step1Executed, step1Compensated int

	require.NoError(t, cmdBus.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		step1Executed++
		return nil
	}))
	require.NoError(t, cmdBus.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		step1Compensated++
		return assert.AnError
	}))
	require.NoError(t, cmdBus.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}

	saga := &failingSaga{}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1", "CmdStep1", 1, "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1-comp", "CmdStep1Comp", 1, "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step2", "CmdStep2", 1, "Step2", nil), nil
		}),
	}

	orchestrator := NewSagaOrchestrator(cmdBus, mockBus, stateStore)
	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSagaStepFailed)

	// 第一步执行一次，补偿尝试一次并失败
	assert.Equal(t, 1, step1Executed)
	assert.Equal(t, 1, step1Compensated)

	// OnFailed 应被调用
	assert.True(t, saga.failedCalled)
	assert.False(t, saga.completedCalled)

	// 状态应保持在失败/补偿失败，而不是已补偿
	state, stateErr := stateStore.Load(ctx, saga.GetID())
	require.NoError(t, stateErr)
	assert.True(t, state.IsFailed() || state.IsCompensating())

	// 事件中应包含 SagaFailed
	var hasSagaFailed bool
	for _, evt := range mockBus.events {
		if evt.GetType() == EventSagaFailed {
			hasSagaFailed = true
			break
		}
	}
	assert.True(t, hasSagaFailed)
}
