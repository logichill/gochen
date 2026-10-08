package saga

import (
	"context"

	"testing"
	"time"

	"gochen/testkit/assert"
	"gochen/testkit/require"

	"gochen/errors"

	"gochen/messaging/command"

	"gochen/process/task"
)

func TestWatchSagaLeaseLostStopsWhenLeaseLostChannelDoesNotClose(t *testing.T) {
	lease := newMockLease()
	_, stopWatching := watchSagaLeaseLost(context.Background(), task.NewTaskSupervisor("process.saga.lease_watch.test"), lease)

	stopped := make(chan struct{})
	go func() {
		stopWatching()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected lease watcher to stop after release even when Lost channel remains open")
	}
}

// TestSagaOrchestrator_WithLockProvider 验证 SagaOrchestrator WithLockProvider。
func TestSagaOrchestrator_WithLockProvider(t *testing.T) {
	cmdExecutor := newTestCommandExecutor()
	mockBus := &mockSagaEventBus{}

	provider := &mockLockProvider{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, nil).WithLockProvider(provider)
	require.Equal(t, provider, orchestrator.lock)
}

func TestSagaOrchestrator_WithLockProviderIgnoresTypedNilAndSupportsNilReceiver(t *testing.T) {
	var typedNil *mockLockProvider
	orchestrator := newTestSagaOrchestrator(t, newTestCommandExecutor(), &mockSagaEventBus{}, nil)

	require.Same(t, orchestrator, orchestrator.WithLockProvider(typedNil))
	require.Nil(t, orchestrator.lock)

	var nilOrchestrator *SagaOrchestrator
	require.Nil(t, nilOrchestrator.WithLockProvider(&mockLockProvider{}))
}

func TestSagaOrchestrator_AcquireSagaLockPreservesErrorCode(t *testing.T) {
	cmdExecutor := newTestCommandExecutor()
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, &mockSagaEventBus{}, nil).
		WithLockProvider(failingSagaLockProvider{err: errors.NewCode(errors.Database, "db down")})

	_, _, err := orchestrator.acquireSagaLock(context.Background(), "saga-db")
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Database), "expected database code, got %v", err)
}

func TestEnsureSagaLeaseActivePreservesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(context.Canceled)

	err := ensureSagaLeaseActive(ctx, "saga-cancel")
	require.ErrorIs(t, err, context.Canceled)
	require.Falsef(t, errors.Is(err, errors.Conflict), "cancellation should not be reported as lease lost: %v", err)
}

func TestSagaOrchestrator_Execute_PreservesLockAcquireErrorCode(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	stateStore := NewMemorySagaStateStore()
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, &mockSagaEventBus{}, stateStore).
		WithLockProvider(failingSagaLockProvider{err: errors.NewCode(errors.Database, "lock db down")})

	err := orchestrator.Execute(ctx, &simpleSaga{})
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Database), "expected database error, got %v", err)
	require.Falsef(t, errors.Is(err, errors.Timeout), "lock database error should not be rewritten to timeout: %v", err)
}

func TestEnsureSagaLeaseActive_ContextCancellationIsNotLeaseLost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ensureSagaLeaseActive(ctx, "saga-cancel")
	require.ErrorIs(t, err, context.Canceled)
	require.Falsef(t, errors.Is(err, errors.Conflict), "caller cancellation should not be reported as lease lost: %v", err)
}

func TestSagaOrchestrator_Execute_DoesNotMarkStepCompletedAfterLeaseLost(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	lease := newMockLease()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
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

	err := orchestrator.Execute(ctx, &simpleSaga{})
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Conflict), "expected lease conflict, got %v", err)

	state, loadErr := stateStore.Load(ctx, "saga-1")
	require.NoError(t, loadErr)
	require.Equal(t, SagaStatusRunning, state.Status)
	require.Equal(t, 0, state.CurrentStep)
	require.Empty(t, state.CompletedSteps)

	counts := countSagaEventTypes(mockBus.events)
	require.Zero(t, counts[string(EventSagaStepCompleted)])
	require.Zero(t, counts[string(EventSagaCompleted)])
}

func TestSagaOrchestrator_Execute_DoesNotHandleStepFailureAfterLeaseLostError(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	lease := newMockLease()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
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

	err := orchestrator.Execute(ctx, &simpleSaga{})
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Conflict), "expected lease conflict, got %v", err)

	state, loadErr := stateStore.Load(ctx, "saga-1")
	require.NoError(t, loadErr)
	require.Equal(t, SagaStatusRunning, state.Status)
	require.Empty(t, state.CompletedSteps)

	counts := countSagaEventTypes(mockBus.events)
	require.Zero(t, counts[string(EventSagaStepFailed)])
	require.Zero(t, counts[string(EventSagaFailed)])
}

func TestSagaOrchestrator_Execute_DoesNotCompleteAfterLeaseLostInOnComplete(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	lease := newMockLease()
	stateStore := NewMemorySagaStateStore()
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: lease})
	saga := &leaseLostOnCompleteSaga{
		id:    "execute-on-complete-lease-lost",
		lease: lease,
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
	require.Zero(t, saga.failedCall)

	state, loadErr := stateStore.Load(ctx, saga.ID())
	require.NoError(t, loadErr)
	require.Equal(t, SagaStatusRunning, state.Status)
	require.Equal(t, []string{"step1"}, state.CompletedSteps)

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaStarted)])
	require.Equal(t, 1, counts[string(EventSagaStepCompleted)])
	require.Zero(t, counts[string(EventSagaCompleted)])
	require.Zero(t, counts[string(EventSagaFailed)])
}

func TestSagaOrchestrator_Execute_DoesNotPublishCompletedAfterLeaseLostDuringTerminalUpdate(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	lease := newMockLease()
	stateStore := &leaseLostOnTerminalUpdateStore{MemorySagaStateStore: NewMemorySagaStateStore(), lease: lease}
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: lease})

	err := orchestrator.Execute(ctx, &simpleSaga{})
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Conflict), "expected lease conflict, got %v", err)

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaStarted)])
	require.Equal(t, 1, counts[string(EventSagaStepCompleted)])
	require.Zero(t, counts[string(EventSagaCompleted)])
	require.Zero(t, counts[string(EventSagaFailed)])
}

func TestSagaOrchestrator_Execute_DoesNotPublishFailedAfterLeaseLostDuringOnCompleteFailure(t *testing.T) {
	ctx := context.Background()
	cmdExecutor := newTestCommandExecutor()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	lease := newMockLease()
	stateStore := &leaseLostOnTerminalUpdateStore{MemorySagaStateStore: NewMemorySagaStateStore(), lease: lease}
	mockBus := &mockSagaEventBus{}
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, mockBus, stateStore).
		WithLockProvider(&mockLeaseLockProvider{lease: lease})
	saga := &completionFailSaga{simpleSaga: &simpleSaga{}}

	err := orchestrator.Execute(ctx, saga)
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.Conflict), "expected lease conflict, got %v", err)
	require.False(t, saga.failedCalled)

	counts := countSagaEventTypes(mockBus.events)
	require.Equal(t, 1, counts[string(EventSagaStarted)])
	require.Equal(t, 1, counts[string(EventSagaStepCompleted)])
	require.Zero(t, counts[string(EventSagaCompleted)])
	require.Zero(t, counts[string(EventSagaFailed)])
}
