package saga

import (
	"context"

	"testing"
	"time"

	"gochen/testkit/assert"
	"gochen/testkit/require"

	"gochen/clock"
	"gochen/errors"

	"gochen/messaging/command"
	"gochen/messaging/command/middleware"
)

func TestSagaOrchestrator_Resume_RecoversAfterOnCompleteFailure(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore)
	// OnComplete 首次失败、第二次成功。
	saga := &completionRecoverSaga{simpleSaga: &simpleSaga{}, failTimes: 1}

	// 首次 Execute：OnComplete 失败 → PendingCompletion，可恢复而非卡死。
	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	state, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Equal(t, SagaStatusPendingCompletion, state.Status)

	// Resume：重新进入 completeSaga，OnComplete 重试成功 → Completed。
	require.NoError(t, orchestrator.Resume(ctx, saga, state))
	state, loadErr = stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Equal(t, SagaStatusCompleted, state.Status)
	require.Equal(t, 2, saga.completeCalls, "resume should retry OnComplete")

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaCompletionFailed)])
	require.Equal(t, 1, counts[string(EventSagaCompleted)])
	require.Zero(t, counts[string(EventSagaFailed)])
}

func TestSagaOrchestrator_ResumeDoesNotMutateCallerState(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	stateStore := NewMemorySagaStateStore()
	manualClock := clock.NewManualClock(time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC))
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, &mockSagaEventBus{}, stateStore).
		WithClock(manualClock)

	saga := &resumeSaga{id: "resume-no-caller-mutation"}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1", "Cmd1", "1", "Step1", nil), nil
		}),
	}
	state := NewSagaState(saga.ID(), "resumeSaga")
	state.Status = SagaStatusRunning
	state.CurrentStep = 1
	state.CompletedSteps = []string{"step1"}
	original := state.Clone()
	require.NoError(t, stateStore.Save(ctx, state))

	require.NoError(t, orchestrator.Resume(ctx, saga, state))

	require.Equal(t, original.Status, state.Status)
	require.Equal(t, original.CurrentStep, state.CurrentStep)
	require.Equal(t, original.CompletedSteps, state.CompletedSteps)
	require.Equal(t, original.UpdatedAt, state.UpdatedAt)

	loaded, err := stateStore.Load(ctx, saga.ID())
	require.NoError(t, err)
	require.True(t, loaded.IsCompleted())
	require.Equal(t, manualClock.Now(), loaded.UpdatedAt)
}

func TestSagaOrchestrator_Resume_DoesNotReplayOnCompleteAfterExecuteLeaseLost(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	initialLease := newMockLease()
	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: initialLease})
	saga := &leaseLostOnCompleteSaga{
		id:    "resume-after-execute-on-complete-lease-lost",
		lease: initialLease,
		steps: []*SagaStep{
			NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
				return command.NewCommand("cmd1", "TestCommand", "1", "Test", nil), nil
			}),
		},
	}

	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Conflict), "expected lease conflict, got %v", err)
	require.Equal(t, 1, saga.completedCall)

	state, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Equal(t, SagaStatusRunning, state.Status)
	persisted, ok := state.GetData(sagaOnCompletePersistedKey)
	require.True(t, ok, "expected persisted on-complete marker")
	require.Equal(t, true, persisted)

	resumeLease := newMockLease()
	saga.lease = resumeLease
	resumeOrchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: resumeLease})

	err = resumeOrchestrator.Resume(ctx, saga, state)
	require.NoError(t, err)
	require.Equal(t, 1, saga.completedCall, "resume should not replay OnComplete")

	loaded, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Equal(t, SagaStatusCompleted, loaded.Status)

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaCompleted)])
}

// TestSagaOrchestrator_Resume_ContinuesFromCurrentStep 验证 SagaOrchestrator Resume ContinuesFromCurrentStep。
func TestSagaOrchestrator_Resume_ContinuesFromCurrentStep(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()

	var cmd1, cmd2 int
	require.NoError(t, cmdExecutor.RegisterHandler("Cmd1", func(ctx context.Context, cmd *command.Command) error {
		cmd1++
		return nil
	}))
	require.NoError(t, cmdExecutor.RegisterHandler("Cmd2", func(ctx context.Context, cmd *command.Command) error {
		cmd2++
		return nil
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	provider := &mockLockProvider{}

	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).WithLockProvider(provider)

	saga := &resumeSaga{id: "resume-1"}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1", "Cmd1", "1", "Step1", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c2", "Cmd2", "1", "Step2", nil), nil
		}),
	}

	// 模拟：step1 已完成，继续从 step2 开始
	state := NewSagaState(saga.ID(), "resumeSaga")
	state.Status = SagaStatusRunning
	state.CurrentStep = 1
	state.CompletedSteps = []string{"step1"}
	require.NoError(t, stateStore.Save(ctx, state))

	require.NoError(t, orchestrator.Resume(ctx, saga, state))

	require.Equal(t, 0, cmd1)
	require.Equal(t, 1, cmd2)
	require.Equal(t, 1, saga.completedCall)
	require.Equal(t, 0, saga.failedCall)
	require.Equal(t, 1, provider.acquired)

	loaded, err := stateStore.Load(ctx, saga.ID())
	require.NoError(t, err)
	require.True(t, loaded.IsCompleted())

	counts := countSagaEventTypes(mockBus.events)
	assert.Equal(t, 1, counts[string(EventSagaResumed)])
	assert.Equal(t, 1, counts[string(EventSagaStepCompleted)])
	assert.Equal(t, 1, counts[string(EventSagaCompleted)])
	assert.Zero(t, counts[string(EventSagaFailed)])
}

// TestSagaOrchestrator_Resume_CompletedIsConflict 验证 SagaOrchestrator Resume CompletedIsConflict。
func TestSagaOrchestrator_Resume_CompletedIsConflict(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore)

	saga := &resumeSaga{id: "resume-2"}
	saga.steps = []*SagaStep{NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
		return command.NewCommand("c1", "Cmd1", "1", "Step1", nil), nil
	})}

	state := NewSagaState(saga.ID(), "resumeSaga")
	state.Status = SagaStatusCompleted
	require.Error(t, orchestrator.Resume(ctx, saga, state))
}

func TestSagaOrchestrator_Resume_InvalidCurrentStep_FailFast(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()
	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore)

	saga := &resumeSaga{id: "resume-invalid-step"}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1", "Cmd1", "1", "Step1", nil), nil
		}),
	}

	testCases := []struct {
		name        string
		currentStep int
	}{
		{name: "negative", currentStep: -1},
		{name: "overflow", currentStep: 2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockBus.events = nil
			state := NewSagaState(saga.ID(), "resumeSaga")
			state.Status = SagaStatusRunning
			state.CurrentStep = tc.currentStep

			err := orchestrator.Resume(ctx, saga, state)
			require.True(t, errors.Is(err, errors.InvalidInput))
			assert.Empty(t, mockBus.events)
			assert.Equal(t, 0, saga.completedCall)
			assert.Equal(t, 0, saga.failedCall)
		})
	}
}

func TestSagaOrchestrator_Resume_InvalidStateConsistency_FailFast(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()
	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore)

	saga := &resumeSaga{id: "resume-invalid-state"}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1", "Cmd1", "1", "Step1", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c2", "Cmd2", "1", "Step2", nil), nil
		}),
	}

	testCases := []struct {
		name  string
		state *SagaState
		code  errors.ErrorCode
	}{
		{
			name:  "nil state",
			state: nil,
			code:  errors.InvalidInput,
		},
		{
			name: "saga id mismatch",
			state: func() *SagaState {
				s := NewSagaState("other-saga", "resumeSaga")
				s.Status = SagaStatusRunning
				return s
			}(),
			code: errors.InvalidInput,
		},
		{
			name: "empty saga id",
			state: func() *SagaState {
				s := NewSagaState("", "resumeSaga")
				s.Status = SagaStatusRunning
				return s
			}(),
			code: errors.InvalidInput,
		},
		{
			name: "pending is not resumable",
			state: func() *SagaState {
				s := NewSagaState(saga.ID(), "resumeSaga")
				s.Status = SagaStatusPending
				return s
			}(),
			code: errors.FailedPrecondition,
		},
		{
			name: "compensating is not resumable",
			state: func() *SagaState {
				s := NewSagaState(saga.ID(), "resumeSaga")
				s.Status = SagaStatusCompensating
				s.CurrentStep = 1
				s.CompletedSteps = []string{"step1"}
				return s
			}(),
			code: errors.Conflict,
		},
		{
			name: "completed steps length mismatch",
			state: func() *SagaState {
				s := NewSagaState(saga.ID(), "resumeSaga")
				s.Status = SagaStatusRunning
				s.CurrentStep = 1
				return s
			}(),
			code: errors.InvalidInput,
		},
		{
			name: "completed steps name mismatch",
			state: func() *SagaState {
				s := NewSagaState(saga.ID(), "resumeSaga")
				s.Status = SagaStatusRunning
				s.CurrentStep = 1
				s.CompletedSteps = []string{"wrong-step"}
				return s
			}(),
			code: errors.InvalidInput,
		},
		{
			name: "pending completion before all steps completed",
			state: func() *SagaState {
				s := NewSagaState(saga.ID(), "resumeSaga")
				s.Status = SagaStatusPendingCompletion
				s.CurrentStep = 1
				s.CompletedSteps = []string{"step1"}
				return s
			}(),
			code: errors.InvalidInput,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockBus.events = nil
			err := orchestrator.Resume(ctx, saga, tc.state)
			require.True(t, errors.Is(err, tc.code))
			assert.Empty(t, mockBus.events)
			assert.Equal(t, 0, saga.completedCall)
			assert.Equal(t, 0, saga.failedCall)
		})
	}
}

func TestSagaOrchestrator_Resume_InvalidSagaDefinition_FailFast(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()
	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore)

	saga := &resumeSaga{id: "resume-invalid-definition"}
	state := NewSagaState(saga.ID(), "resumeSaga")
	state.Status = SagaStatusRunning

	err := orchestrator.Resume(ctx, saga, state)
	require.True(t, errors.Is(err, errors.InvalidInput))
	assert.Empty(t, mockBus.events)
}

func TestSagaOrchestrator_Resume_DoesNotReplayStepCommandAfterPersistFailure(t *testing.T) {
	ctx := context.Background()

	cmdExecutor := newTestCommandExecutor()
	idempotency := middleware.NewIdempotencyMiddleware(nil)
	defer func() { _ = idempotency.Stop(context.Background()) }()
	cmdExecutor.Use(idempotency)

	var step2Executed int
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep2", func(ctx context.Context, cmd *command.Command) error {
		step2Executed++
		key, ok := cmd.GetMetadata().GetString(command.MetadataIdempotencyKey)
		require.True(t, ok)
		require.Equal(t, "saga.step.cmVzdW1lLWlkZW1wb3RlbnQ.c3RlcDI", key)
		return nil
	}))

	stateStore := &failOnceStateStore{MemorySagaStateStore: NewMemorySagaStateStore()}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, &mockSagaEventBus{}, stateStore)

	saga := &resumeSaga{id: "resume-idempotent"}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1", "CmdStep1", "1", "Step1", nil), nil
		}),
		NewSagaStep("step2", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("new-command-id-each-resume", "CmdStep2", "1", "Step2", nil), nil
		}),
	}

	state := NewSagaState(saga.ID(), "resumeSaga")
	state.Status = SagaStatusRunning
	state.CurrentStep = 1
	state.CompletedSteps = []string{"step1"}
	require.NoError(t, stateStore.Save(ctx, state))

	err := orchestrator.Resume(ctx, saga, state.Clone())
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Database))
	require.Equal(t, 1, step2Executed)

	persisted, err := stateStore.Load(ctx, saga.ID())
	require.NoError(t, err)
	require.Equal(t, 1, persisted.CurrentStep)

	require.NoError(t, orchestrator.Resume(ctx, saga, persisted))
	require.Equal(t, 1, step2Executed)

	loaded, err := stateStore.Load(ctx, saga.ID())
	require.NoError(t, err)
	require.True(t, loaded.IsCompleted())
}

func TestSagaOrchestrator_Resume_DoesNotMarkStepCompletedAfterLeaseLost(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	lease := newMockLease()
	require.NoError(t, cmdExecutor.RegisterHandler("CmdStep1", func(ctx context.Context, cmd *command.Command) error {
		lease.lose(assert.AnError)
		deadline := time.Now().Add(time.Second)
		for context.Cause(ctx) == nil && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		return nil
	}))

	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: lease})

	saga := &resumeSaga{id: "resume-lease-lost"}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1", "CmdStep1", "1", "Step1", nil), nil
		}),
	}
	state := NewSagaState(saga.ID(), "resumeSaga")
	state.Status = SagaStatusRunning
	require.NoError(t, stateStore.Save(ctx, state))

	err := orchestrator.Resume(ctx, saga, state)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Conflict), "expected lease conflict, got %v", err)

	loaded, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Equal(t, SagaStatusRunning, loaded.Status)
	require.Equal(t, 0, loaded.CurrentStep)
	require.Empty(t, loaded.CompletedSteps)

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaResumed)])
	require.Zero(t, counts[string(EventSagaStepCompleted)])
	require.Zero(t, counts[string(EventSagaCompleted)])
}

func TestSagaOrchestrator_Resume_DoesNotCompleteAfterLeaseLostInOnComplete(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()

	lease := newMockLease()
	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: lease})
	saga := &leaseLostOnCompleteSaga{
		id:    "resume-on-complete-lease-lost",
		lease: lease,
		steps: []*SagaStep{
			NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
				return command.NewCommand("cmd1", "UnusedCommand", "1", "Step1", nil), nil
			}),
		},
	}
	state := NewSagaState(saga.ID(), "leaseLostOnCompleteSaga")
	state.Status = SagaStatusRunning
	state.CurrentStep = 1
	state.CompletedSteps = []string{"step1"}
	require.NoError(t, stateStore.Save(ctx, state))

	err := orchestrator.Resume(ctx, saga, state)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Conflict), "expected lease conflict, got %v", err)
	require.Equal(t, 1, saga.completedCall)
	require.Zero(t, saga.failedCall)

	loaded, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Equal(t, SagaStatusRunning, loaded.Status)
	require.Equal(t, []string{"step1"}, loaded.CompletedSteps)

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaResumed)])
	require.Zero(t, counts[string(EventSagaCompleted)])
	require.Zero(t, counts[string(EventSagaFailed)])
}
