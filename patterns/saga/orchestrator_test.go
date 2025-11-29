package saga

import (
	"context"
	"testing"

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
