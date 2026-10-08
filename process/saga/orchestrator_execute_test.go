package saga

import (
	"context"

	"testing"
	"time"

	"gochen/testkit/assert"
	"gochen/testkit/require"

	"gochen/clock"
	"gochen/errors"

	"gochen/messaging"
	"gochen/messaging/command"
)

func TestSagaOrchestrator_ExecuteUsesInjectedClockForInitialState(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	stateStore := NewMemorySagaStateStore()
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, &mockSagaEventBus{}, stateStore)

	before := time.Now()
	require.NoError(t, orchestrator.Execute(ctx, &simpleSaga{}))
	after := time.Now()

	state, err := stateStore.Load(ctx, "saga-1")
	require.NoError(t, err)
	require.False(t, state.CreatedAt.Before(before))
	require.False(t, state.CreatedAt.After(after))
	require.False(t, state.UpdatedAt.Before(before), "updated_at should use injected clock or later state transitions")
}

// TestSagaOrchestrator_PublishEvents 验证 SagaOrchestrator PublishEvents。
func TestSagaOrchestrator_PublishEvents(t *testing.T) {
	ctx := context.Background()

	// 显式命令执行端口直接在当前调用栈执行，保证立即拿到真实处理结果
	cmdExecutor := newTestCommandExecutor()

	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	mockBus := &mockSagaEventBus{}

	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, nil)
	err := orchestrator.Execute(ctx, &simpleSaga{})
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(mockBus.events), 3) // Started + StepCompleted + Completed 至少 3 个
	for _, evt := range mockBus.events {
		v, ok := evt.GetMetadata().GetString("saga_id")
		require.True(t, ok)
		require.Equal(t, "saga-1", v)
	}
}

func TestSagaOrchestrator_Execute_ExistingSagaIsConflict(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	stateStore := NewMemorySagaStateStore()
	require.NoError(t, stateStore.Save(ctx, NewSagaState("saga-1", "existing")))

	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, &mockSagaEventBus{}, stateStore)
	err := orchestrator.Execute(ctx, &simpleSaga{})
	require.True(t, errors.Is(err, errors.Conflict))
}

func TestSagaOrchestrator_Execute_InitialStateUsesInjectedClock(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))
	start := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	manualClock := clock.NewManualClock(start)
	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithClock(manualClock)

	require.NoError(t, orchestrator.Execute(ctx, &simpleSaga{}))
	state, err := stateStore.Load(ctx, "saga-1")
	require.NoError(t, err)
	require.Equal(t, start, state.CreatedAt)
	require.Equal(t, start, state.UpdatedAt)
	require.NotEmpty(t, mockBus.events)
	for _, evt := range mockBus.events {
		payload, ok := messaging.PayloadValue(evt.GetPayload()).(sagaEventPayload)
		require.Truef(t, ok, "unexpected saga event payload: %#v", messaging.PayloadValue(evt.GetPayload()))
		require.Equal(t, start, payload.Timestamp)
	}
}

func TestSagaOrchestrator_Execute_OnCompleteFailureMarksPendingCompletion(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore)
	saga := &completionFailSaga{simpleSaga: &simpleSaga{}}

	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Internal), "expected internal error, got %v", err)
	// OnComplete 失败属可恢复，不应触发 OnFailed（saga 未失败）。
	require.False(t, saga.failedCalled)

	state, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Equal(t, SagaStatusPendingCompletion, state.Status)
	require.Equal(t, assert.AnError.Error(), state.Error)
	require.Equal(t, []string{"step1"}, state.CompletedSteps)

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaStarted)])
	require.Equal(t, 1, counts[string(EventSagaStepCompleted)])
	require.Equal(t, 1, counts[string(EventSagaCompletionFailed)])
	require.Zero(t, counts[string(EventSagaFailed)])
	require.Zero(t, counts[string(EventSagaCompleted)])
}

func TestSagaOrchestrator_Execute_InvalidSagaSteps_FailFast(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, &mockSagaEventBus{}, nil)

	testCases := []struct {
		name string
		saga ISaga
	}{
		{
			name: "no steps",
			saga: &resumeSaga{
				id: "invalid-step-0",
			},
		},
		{
			name: "nil step",
			saga: &resumeSaga{
				id:    "invalid-step-1",
				steps: []*SagaStep{nil},
			},
		},
		{
			name: "empty step name",
			saga: &resumeSaga{
				id: "invalid-step-2",
				steps: []*SagaStep{
					{
						Name: "",
						Command: func(ctx context.Context) (*command.Command, error) {
							return command.NewCommand("c1", "Cmd1", "1", "Step1", nil), nil
						},
					},
				},
			},
		},
		{
			name: "duplicate step names",
			saga: &resumeSaga{
				id: "invalid-step-4",
				steps: []*SagaStep{
					NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
						return command.NewCommand("c1", "Cmd1", "1", "Step1", nil), nil
					}),
					NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
						return command.NewCommand("c2", "Cmd2", "1", "Step2", nil), nil
					}),
				},
			},
		},
		{
			name: "nil command",
			saga: &resumeSaga{
				id: "invalid-step-3",
				steps: []*SagaStep{
					{Name: "step1"},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := orchestrator.Execute(ctx, tc.saga)
			require.True(t, errors.Is(err, errors.InvalidInput))
		})
	}
}
