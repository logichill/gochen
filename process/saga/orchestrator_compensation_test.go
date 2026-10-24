package saga

import (
	"context"

	"testing"
	"time"

	"gochen/testkit/assert"
	"gochen/testkit/require"

	"gochen/errors"

	"gochen/messaging"
	"gochen/messaging/command"
)

// TestSagaOrchestrator_Resume_Failure_WithSuccessfulCompensation_EmitsLifecycleEvents 验证 Resume 失败补偿路径也会发布完整生命周期事件。
func TestSagaOrchestrator_Resume_Failure_WithSuccessfulCompensation_EmitsLifecycleEvents(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()

	var step1Compensated, step2Executed int
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		step1Compensated++
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		step2Executed++
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore)

	saga := &resumeSaga{id: "resume-fail-1"}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1", "Cmd1", "1", "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1-comp", "CmdStep1Comp", "1", "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c2", "CmdStep2", "1", "Step2", nil), nil
		}),
	}

	state := NewSagaState(saga.ID(), "resumeSaga")
	state.Status = SagaStatusRunning
	state.CurrentStep = 1
	state.CompletedSteps = []string{"step1"}
	require.NoError(t, stateStore.Save(ctx, state))

	err := orchestrator.Resume(ctx, saga, state)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Internal))
	assert.Equal(t, 1, step2Executed)
	assert.Equal(t, 1, step1Compensated)
	assert.Equal(t, 0, saga.completedCall)
	assert.Equal(t, 1, saga.failedCall)

	loaded, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	assert.True(t, loaded.IsCompensated())

	counts := countSagaEventTypes(mockBus.events)
	assert.Equal(t, 1, counts[string(EventSagaResumed)])
	assert.Equal(t, 1, counts[string(EventSagaStepFailed)])
	assert.Equal(t, 1, counts[string(EventSagaCompensationStarted)])
	assert.Equal(t, 1, counts[string(EventSagaCompensationStepCompleted)])
	assert.Equal(t, 1, counts[string(EventSagaCompensationCompleted)])
	assert.Zero(t, counts[string(EventSagaCompleted)])
	assert.Zero(t, counts[string(EventSagaFailed)])
}

func TestSagaOrchestrator_Resume_DoesNotCompensateWhenStepFailsAfterLeaseLost(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	lease := newMockLease()
	var step2Executed, step1Compensated int

	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		step1Compensated++
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		step2Executed++
		lease.lose(assert.AnError)
		deadline := time.Now().Add(time.Second)
		for context.Cause(ctx) == nil && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: lease})

	saga := &resumeSaga{id: "resume-lease-lost-fail"}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1", "CmdStep1", "1", "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1-comp", "CmdStep1Comp", "1", "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c2", "CmdStep2", "1", "Step2", nil), nil
		}),
	}
	state := NewSagaState(saga.ID(), "resumeSaga")
	state.Status = SagaStatusRunning
	state.CurrentStep = 1
	state.CompletedSteps = []string{"step1"}
	require.NoError(t, stateStore.Save(ctx, state))

	err := orchestrator.Resume(ctx, saga, state)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Conflict), "expected lease conflict, got %v", err)
	require.Equal(t, 1, step2Executed)
	require.Zero(t, step1Compensated)

	loaded, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Equal(t, SagaStatusRunning, loaded.Status)
	require.Equal(t, 1, loaded.CurrentStep)
	require.Equal(t, []string{"step1"}, loaded.CompletedSteps)

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaResumed)])
	require.Zero(t, counts[string(EventSagaStepFailed)])
	require.Zero(t, counts[string(EventSagaCompensationStarted)])
	require.Zero(t, counts[string(EventSagaCompensationStepCompleted)])
}

// TestSagaOrchestrator_StepFailure_WithSuccessfulCompensation 验证 SagaOrchestrator StepFailure WithSuccessfulCompensation。
func TestSagaOrchestrator_StepFailure_WithSuccessfulCompensation(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()

	var step1Executed, step1Compensated, step2Executed int

	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		step1Executed++
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		step1Compensated++
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		step2Executed++
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}

	saga := &failingSaga{}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1", "CmdStep1", "1", "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1-comp", "CmdStep1Comp", "1", "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step2", "CmdStep2", "1", "Step2", nil), nil
		}),
	}

	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore)
	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Internal))
	assert.Contains(t, err.Error(), "saga step failed")

	// 步骤执行与补偿情况
	assert.Equal(t, 1, step1Executed)
	assert.Equal(t, 1, step2Executed)
	assert.Equal(t, 1, step1Compensated)

	// OnFailed 应被调用，而 OnComplete 不应被调用
	assert.True(t, saga.failedCalled)
	assert.False(t, saga.completedCalled)

	// 状态应被标记为已补偿
	state, stateErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, stateErr)
	assert.True(t, state.IsCompensated())

	// 事件总线中应包含步骤失败、补偿步骤完成与补偿完成事件。
	var hasStepFailed, hasCompStepCompleted, hasCompCompleted bool
	var stepCompletedCount int
	for _, evt := range mockBus.events {
		switch evt.GetType() {
		case string(EventSagaStepFailed):
			hasStepFailed = true
		case string(EventSagaStepCompleted):
			stepCompletedCount++
		case string(EventSagaCompensationStepCompleted):
			hasCompStepCompleted = true
		case string(EventSagaCompensationCompleted):
			hasCompCompleted = true
		}
	}
	assert.True(t, hasStepFailed)
	assert.True(t, hasCompStepCompleted)
	assert.True(t, hasCompCompleted)
	assert.Equal(t, 1, stepCompletedCount)
}

func TestSagaOrchestrator_FirstStepFailureSkipsCompensationLifecycleEvents(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	var stepExecuted, compensationGenerated int
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		stepExecuted++
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	saga := &failingSaga{}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1", "CmdStep1", "1", "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			compensationGenerated++
			return command.NewCommand("cmd-step1-comp", "CmdStep1Comp", "1", "Step1Comp", nil), nil
		}),
	}

	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore)
	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Internal))
	require.Equal(t, 1, stepExecuted)
	require.Zero(t, compensationGenerated)
	require.True(t, saga.failedCalled)

	state, stateErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, stateErr)
	require.True(t, state.IsFailed())

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaStepFailed)])
	require.Zero(t, counts[string(EventSagaCompensationStarted)])
	require.Zero(t, counts[string(EventSagaCompensationStepCompleted)])
	require.Zero(t, counts[string(EventSagaCompensationCompleted)])
	require.Equal(t, 1, counts[string(EventSagaFailed)])
}

func TestSagaOrchestrator_CompensationStopsWhenLeaseLost(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()

	var step1Executed, step1Compensated, step2Executed int
	lease := newMockLease()

	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		step1Executed++
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		step1Compensated++
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		step2Executed++
		lease.lose(assert.AnError)
		deadline := time.Now().Add(time.Second)
		for context.Cause(ctx) == nil && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}

	saga := &failingSaga{}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1", "CmdStep1", "1", "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1-comp", "CmdStep1Comp", "1", "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step2", "CmdStep2", "1", "Step2", nil), nil
		}),
	}

	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: lease})
	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	assert.Truef(t, errors.Is(err, errors.Conflict), "expected lease conflict, got %v", err)

	assert.Equal(t, 1, step1Executed)
	assert.Equal(t, 1, step2Executed)
	assert.Equal(t, 0, step1Compensated)

	counts := countSagaEventTypes(mockBus.events)
	assert.Zero(t, counts[string(EventSagaStepFailed)])
	assert.Zero(t, counts[string(EventSagaCompensationStarted)])
	assert.Zero(t, counts[string(EventSagaCompensationStepCompleted)])
	assert.Zero(t, counts[string(EventSagaFailed)])
}

func TestSagaOrchestrator_CompensationDoesNotEmitFailureAfterLeaseLostError(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	lease := newMockLease()
	var step1Compensated int

	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		step1Compensated++
		lease.lose(assert.AnError)
		deadline := time.Now().Add(time.Second)
		for context.Cause(ctx) == nil && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		return assert.AnError
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	saga := &failingSaga{}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1", "CmdStep1", "1", "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1-comp", "CmdStep1Comp", "1", "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step2", "CmdStep2", "1", "Step2", nil), nil
		}),
	}

	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: lease})
	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Conflict), "expected lease conflict, got %v", err)
	require.Equal(t, 1, step1Compensated)

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaCompensationStarted)])
	require.Zero(t, counts[string(EventSagaCompensationStepFailed)])
	require.Zero(t, counts[string(EventSagaCompensationCompleted)])
	require.Zero(t, counts[string(EventSagaFailed)])
}

// TestSagaOrchestrator_CompensationFailure_EmitsSagaFailed 验证 SagaOrchestrator CompensationFailure EmitsSagaFailed。
func TestSagaOrchestrator_CompensationFailure_EmitsSagaFailed(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()

	var step1Executed, step1Compensated int

	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		step1Executed++
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		step1Compensated++
		return assert.AnError
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}

	saga := &failingSaga{}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1", "CmdStep1", "1", "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1-comp", "CmdStep1Comp", "1", "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step2", "CmdStep2", "1", "Step2", nil), nil
		}),
	}

	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore)
	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Internal))
	assert.Contains(t, err.Error(), "saga step failed")

	// 第一步执行一次，补偿尝试一次并失败
	assert.Equal(t, 1, step1Executed)
	assert.Equal(t, 1, step1Compensated)

	// OnFailed 应被调用
	assert.True(t, saga.failedCalled)
	assert.False(t, saga.completedCalled)

	// 状态应保持在失败/补偿失败，而不是已补偿
	state, stateErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, stateErr)
	assert.True(t, state.IsFailed())
	assert.False(t, state.IsCompensating())

	// 事件中应包含 SagaFailed
	var hasSagaFailed bool
	for _, evt := range mockBus.events {
		if evt.GetType() == string(EventSagaFailed) {
			hasSagaFailed = true
			payload, ok := messaging.PayloadValue(evt.GetPayload()).(sagaEventPayload)
			require.Truef(t, ok, "unexpected saga failed payload: %#v", messaging.PayloadValue(evt.GetPayload()))
			require.Equal(t, "saga execution failed", payload.Error)
			require.Equal(t, "saga execution failed", payload.Extra["error"])
			require.Equal(t, string(errors.Internal), payload.Extra["error_code"])
			require.Equal(t, "compensation execution failed", payload.Extra["compensation_error"])
			require.Equal(t, string(errors.Internal), payload.Extra["compensation_error_code"])
			require.NotContains(t, payload.Error, "assert.AnError")
			require.NotContains(t, payload.Extra["error"], "assert.AnError")
			require.NotContains(t, payload.Extra["compensation_error"], "assert.AnError")
			break
		}
	}
	assert.True(t, hasSagaFailed)
}

func TestSagaOrchestrator_CompensationStatePersistFailure_PreservesCompensatedSemantics(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()

	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		return assert.AnError
	}))

	stateStore := &failOnCompensatedStateStore{MemorySagaStateStore: NewMemorySagaStateStore()}
	mockBus := &mockSagaEventBus{}

	saga := &failingSaga{}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1", "CmdStep1", "1", "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1-comp", "CmdStep1Comp", "1", "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step2", "CmdStep2", "1", "Step2", nil), nil
		}),
	}

	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore)
	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Database))
	assert.True(t, saga.failedCalled)
	assert.False(t, saga.completedCalled)

	loaded, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	assert.True(t, loaded.IsCompensating())

	counts := countSagaEventTypes(mockBus.events)
	assert.Equal(t, 1, counts[string(EventSagaStepFailed)])
	assert.Equal(t, 1, counts[string(EventSagaCompensationStarted)])
	assert.Equal(t, 1, counts[string(EventSagaCompensationStepCompleted)])
	assert.Equal(t, 1, counts[string(EventSagaCompensationCompleted)])
	assert.Zero(t, counts[string(EventSagaFailed)])
}

// waitContextCause 阻塞等待 ctx 的 Cause 非空（最长 2 秒），供测试模拟
// 父取消/租约丢失信号在协程间传播。
func waitContextCause(ctx context.Context) {
	deadline := time.Now().Add(2 * time.Second)
	for context.Cause(ctx) == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

// TestSagaOrchestrator_CompensationAbortsWhenLeaseLostAfterParentCancel 钉住
// 补偿上下文与父取消解绑后的租约保护：请求 ctx 已取消时补偿仍会执行，
// 但补偿期间租约失效必须中止补偿，而不是在无租约状态下继续下发补偿命令。
// （回归：WithoutCancel 剥离租约检测后该场景补偿会完整执行。）
func TestSagaOrchestrator_CompensationAbortsWhenLeaseLostAfterParentCancel(t *testing.T) {
	ctx, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	cmdExecutor := newTestCommandExecutor()
	lease := newMockLease()
	var step1Compensated int

	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		step1Compensated++
		lease.lose(assert.AnError)
		waitContextCause(ctx) // 补偿专用 ctx 必须收到租约丢失信号
		return assert.AnError
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		cancelParent() // 常规请求取消
		waitContextCause(ctx)
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: lease})

	saga := &failingSaga{}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1", "CmdStep1", "1", "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1-comp", "CmdStep1Comp", "1", "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step2", "CmdStep2", "1", "Step2", nil), nil
		}),
	}

	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Conflict), "expected lease conflict, got %v", err)
	require.Equal(t, 1, step1Compensated)

	loaded, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Truef(t, loaded.IsCompensating(), "expected compensating state, got %v", loaded.Status)

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaCompensationStarted)])
	require.Zero(t, counts[string(EventSagaCompensationStepCompleted)])
	require.Zero(t, counts[string(EventSagaCompensationCompleted)])
	require.Zero(t, counts[string(EventSagaFailed)])
}

// TestSagaOrchestrator_NormalCancelStillCompensates 钉住常规取消不再被前置
// 守卫短路：步骤失败源于父 ctx 取消（非租约丢失）时，失败处理与补偿必须
// 继续执行并完成回滚。
// （回归：旧实现把普通取消当作租约错误直接返回，补偿从不执行。）
func TestSagaOrchestrator_NormalCancelStillCompensates(t *testing.T) {
	ctx, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	cmdExecutor := newTestCommandExecutor()
	var step1Compensated int

	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		step1Compensated++
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		cancelParent()
		waitContextCause(ctx)
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: newMockLease()})

	saga := &failingSaga{}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1", "CmdStep1", "1", "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1-comp", "CmdStep1Comp", "1", "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step2", "CmdStep2", "1", "Step2", nil), nil
		}),
	}

	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Internal), "expected saga step failure, got %v", err)
	require.Contains(t, err.Error(), "saga step failed")
	require.Equal(t, 1, step1Compensated)

	loaded, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Truef(t, loaded.IsCompensated(), "expected compensated state, got %v", loaded.Status)

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaCompensationStarted)])
	require.Equal(t, 1, counts[string(EventSagaCompensationStepCompleted)])
	require.Equal(t, 1, counts[string(EventSagaCompensationCompleted)])
}

// TestSagaOrchestrator_CompensationEnforcesDedicatedTimeout 钉住补偿专用超时：
// 补偿命令长时间不返回时，超时上限兜底中止补偿并进入常规补偿失败路径
// （终态 Failed + SagaFailed 事件），不会无限期执行。
// （回归：WithoutCancel detach 丢弃父 deadline 且无任何补偿超时。）
func TestSagaOrchestrator_CompensationEnforcesDedicatedTimeout(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()
	var step1Compensated int

	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1Comp", func(ctx context.Context, cmd *command.Command) error {
		step1Compensated++
		<-ctx.Done() // 模拟补偿命令挂死，直到补偿超时
		return ctx.Err()
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		return assert.AnError
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithCompensationTimeout(50 * time.Millisecond)

	saga := &failingSaga{}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1", "CmdStep1", "1", "Step1", nil), nil
		}).WithCompensation(func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step1-comp", "CmdStep1Comp", "1", "Step1Comp", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd-step2", "CmdStep2", "1", "Step2", nil), nil
		}),
	}

	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	// 超时中止的补偿进入常规补偿失败路径：Internal + 底层携带 DeadlineExceeded。
	require.Truef(t, errors.Is(err, errors.Internal), "expected internal failure, got %v", err)
	require.Truef(t, errors.Is(err, context.DeadlineExceeded), "expected deadline cause, got %v", err)
	require.Contains(t, err.Error(), "compensation failed")
	require.Equal(t, 1, step1Compensated)

	loaded, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Truef(t, loaded.IsFailed(), "expected failed state, got %v", loaded.Status)
	require.False(t, loaded.IsCompensated())

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaCompensationStarted)])
	require.Zero(t, counts[string(EventSagaCompensationStepCompleted)])
	require.Zero(t, counts[string(EventSagaCompensationCompleted)])
	require.Equal(t, 1, counts[string(EventSagaFailed)])
}
