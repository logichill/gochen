package saga

import (
	"context"
	stdErrors "errors"
	"testing"
	"time"

	"gochen/testkit/assert"
	"gochen/testkit/require"

	"gochen/clock"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/gen"
	"gochen/messaging"
	"gochen/messaging/command"
	"gochen/messaging/command/middleware"
	"gochen/observe/logging"
	"gochen/process/lock"
	"gochen/process/task"
)

type mockSagaEventBus struct {
	events []eventing.IEvent
}

func newTestSagaOrchestrator(
	t testing.TB,
	commandExecutor command.ICommandExecutor,
	eventBus bus.IEventBus,
	stateStore ISagaStateStore,
) *SagaOrchestrator {
	t.Helper()
	orchestrator, err := NewSagaOrchestrator(
		commandExecutor,
		eventBus,
		stateStore,
		gen.NewUUIDGenerator(),
		logging.NewNoopLogger(),
	)
	require.NoError(t, err)
	return orchestrator
}

func countSagaEventTypes(events []eventing.IEvent) map[string]int {
	counts := make(map[string]int, len(events))
	for _, evt := range events {
		counts[evt.GetType()]++
	}
	return counts
}

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

// PublishEvent 发布事件到事件总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - evt：事件数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *mockSagaEventBus) PublishEvent(ctx context.Context, evt eventing.IEvent) error {
	m.events = append(m.events, evt)
	return nil
}

// PublishEvents 批量发布事件到事件总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - events：事件数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *mockSagaEventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	for _, evt := range events {
		_ = m.PublishEvent(ctx, evt)
	}
	return nil
}

// Publish 发布消息到消息总线。
//
// 说明：
// - Unused in tests
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - message：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *mockSagaEventBus) Publish(ctx context.Context, message messaging.IMessage) error { return nil }

// PublishAll 发布消息到消息总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - messages：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *mockSagaEventBus) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	return nil
}

// SubscribeEvent 订阅指定类型的事件并注册处理器。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - eventType：事件类型
// - handler：事件处理器
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (m *mockSagaEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

// SubscribeHandler 按处理器声明的事件类型批量订阅。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - handler：事件处理器
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (m *mockSagaEventBus) SubscribeHandler(ctx context.Context, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

// Subscribe 订阅消息并注册处理器。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - messageType：参数值（具体语义见函数上下文）（类型：string）
// - handler：事件处理器
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (m *mockSagaEventBus) Subscribe(ctx context.Context, messageType string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

// Use 追加中间件。
//
// 参数：
// - middleware：中间件列表（类型：messaging.IMiddleware）
func (m *mockSagaEventBus) Use(middleware messaging.IMiddleware) {}

var _ bus.IEventBus = (*mockSagaEventBus)(nil)

type mockLockProvider struct {
	acquired int
}

type mockLeaseLockProvider struct {
	acquired int
	lease    *mockLease
	err      error
}

type mockLease struct {
	lost chan error
}

func newMockLease() *mockLease {
	return &mockLease{lost: make(chan error, 1)}
}

func (l *mockLease) Release() {}

func (l *mockLease) Lost() <-chan error {
	return l.lost
}

func (l *mockLease) lose(err error) {
	select {
	case l.lost <- err:
	default:
	}
}

func (p *mockLeaseLockProvider) Acquire(ctx context.Context, key string) (func(), error) {
	lease, err := p.AcquireLease(ctx, key)
	if err != nil {
		return nil, err
	}
	return lease.Release, nil
}

type failOnCompensatedStateStore struct {
	*MemorySagaStateStore
}

func (s *failOnCompensatedStateStore) Update(ctx context.Context, state *SagaState) error {
	if state != nil && state.Status == SagaStatusCompensated {
		return errors.NewCode(errors.Database, "persist compensated state failed")
	}
	return s.MemorySagaStateStore.Update(ctx, state)
}

type failOnceStateStore struct {
	*MemorySagaStateStore
	failed bool
}

func (s *failOnceStateStore) Update(ctx context.Context, state *SagaState) error {
	if !s.failed {
		s.failed = true
		return errors.NewCode(errors.Database, "persist step state failed")
	}
	return s.MemorySagaStateStore.Update(ctx, state)
}

type leaseLostOnTerminalUpdateStore struct {
	*MemorySagaStateStore
	lease *mockLease
}

func (s *leaseLostOnTerminalUpdateStore) Update(ctx context.Context, state *SagaState) error {
	if err := s.MemorySagaStateStore.Update(ctx, state); err != nil {
		return err
	}
	if state != nil && (state.Status == SagaStatusCompleted || state.Status == SagaStatusFailed || state.Status == SagaStatusPendingCompletion) {
		s.lease.lose(assert.AnError)
		deadline := time.Now().Add(time.Second)
		for context.Cause(ctx) == nil && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	}
	return nil
}

// Acquire ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - key：键（用于索引/查找）（类型：string）
//
// 返回：
// - result1：返回结果（类型：func()）
// - err：错误信息（nil 表示成功）
func (p *mockLockProvider) Acquire(ctx context.Context, key string) (func(), error) {
	_ = ctx
	_ = key
	p.acquired++
	return func() {}, nil
}

var _ lock.ILockProvider = (*mockLockProvider)(nil)

func (p *mockLeaseLockProvider) AcquireLease(ctx context.Context, key string) (lock.ILockLease, error) {
	_ = ctx
	_ = key
	p.acquired++
	if p.err != nil {
		return nil, p.err
	}
	if p.lease == nil {
		p.lease = newMockLease()
	}
	return p.lease, nil
}

var _ lock.ILeaseLockProvider = (*mockLeaseLockProvider)(nil)

type failingSagaLockProvider struct {
	err error
}

func (p failingSagaLockProvider) Acquire(context.Context, string) (func(), error) {
	return nil, p.err
}

var _ lock.ILockProvider = failingSagaLockProvider{}

func newTestCommandExecutor() *command.CommandExecutor {
	return command.NewCommandExecutor()
}

// simpleSaga 用于测试的最小 Saga
type simpleSaga struct{}

// ID 返回当前值。
//
// 返回：
// - result：文本结果
func (s *simpleSaga) ID() string { return "saga-1" }

// Steps 返回当前值。
//
// 返回：
// - result：列表结果（元素类型：*SagaStep）
func (s *simpleSaga) Steps() []*SagaStep {
	return []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("cmd1", "TestCommand", "1", "Test", nil), nil
		}),
	}
}

// OnComplete ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *simpleSaga) OnComplete(ctx context.Context) error { return nil }

// OnFailed ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - err：待检查/包装的错误（类型：error）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *simpleSaga) OnFailed(ctx context.Context, err error) error {
	return nil
}

type completionFailSaga struct {
	*simpleSaga
	failedCalled bool
}

func (s *completionFailSaga) OnComplete(ctx context.Context) error {
	return assert.AnError
}

func (s *completionFailSaga) OnFailed(ctx context.Context, err error) error {
	s.failedCalled = true
	return nil
}

// completionRecoverSaga 的 OnComplete 在前 failTimes 次返回错误，之后成功，用于验证 Resume 恢复。
type completionRecoverSaga struct {
	*simpleSaga
	failTimes     int
	completeCalls int
}

func (s *completionRecoverSaga) OnComplete(ctx context.Context) error {
	s.completeCalls++
	if s.completeCalls <= s.failTimes {
		return assert.AnError
	}
	return nil
}

type leaseLostOnCompleteSaga struct {
	id            string
	steps         []*SagaStep
	lease         *mockLease
	completedCall int
	failedCall    int
}

func (s *leaseLostOnCompleteSaga) ID() string { return s.id }

func (s *leaseLostOnCompleteSaga) Steps() []*SagaStep { return s.steps }

func (s *leaseLostOnCompleteSaga) OnComplete(ctx context.Context) error {
	s.completedCall++
	s.lease.lose(assert.AnError)
	deadline := time.Now().Add(time.Second)
	for context.Cause(ctx) == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	return nil
}

func (s *leaseLostOnCompleteSaga) OnFailed(ctx context.Context, err error) error {
	_ = ctx
	_ = err
	s.failedCall++
	return nil
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

func TestNewSagaOrchestratorNormalizesOptionalTypedNilDependencies(t *testing.T) {
	var commandExecutor *command.CommandExecutor
	var eventBus *bus.EventBus
	var stateStore *MemorySagaStateStore

	orchestrator, err := NewSagaOrchestrator(
		commandExecutor,
		eventBus,
		stateStore,
		gen.NewUUIDGenerator(),
		logging.NewNoopLogger(),
	)
	require.NoError(t, err)
	require.Nil(t, orchestrator.commandExecutor)
	require.Nil(t, orchestrator.eventBus)
	require.Nil(t, orchestrator.stateStore)

	err = orchestrator.Execute(context.Background(), &simpleSaga{})
	require.True(t, errors.Is(err, errors.InvalidInput))
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

type resumeSaga struct {
	BaseSaga
	id            string
	steps         []*SagaStep
	completedCall int
	failedCall    int
}

// ID 返回当前值。
//
// 返回：
// - result：文本结果
func (s *resumeSaga) ID() string { return s.id }

// Steps 返回当前值。
//
// 返回：
// - result：列表结果（元素类型：*SagaStep）
func (s *resumeSaga) Steps() []*SagaStep {
	return s.steps
}

// OnComplete ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *resumeSaga) OnComplete(ctx context.Context) error {
	s.completedCall++
	return nil
}

// OnFailed ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - err：待检查/包装的错误（类型：error）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *resumeSaga) OnFailed(ctx context.Context, err error) error {
	_ = err
	s.failedCall++
	return nil
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

func TestSagaOrchestrator_InvalidInput_FailFast(t *testing.T) {
	cmdExecutor := newTestCommandExecutor()
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, &mockSagaEventBus{}, NewMemorySagaStateStore())

	err := orchestrator.Execute(nil, &simpleSaga{})
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = orchestrator.Execute(context.Background(), nil)
	require.True(t, errors.Is(err, errors.InvalidInput))

	saga := &resumeSaga{id: "resume-invalid-input"}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1", "Cmd1", "1", "Step1", nil), nil
		}),
	}

	err = orchestrator.Resume(nil, saga, NewSagaState(saga.ID(), "resumeSaga"))
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = orchestrator.Resume(context.Background(), nil, NewSagaState("resume-invalid-input", "resumeSaga"))
	require.True(t, errors.Is(err, errors.InvalidInput))
}

type stubSagaStringGenerator struct{}

func (s *stubSagaStringGenerator) Next() (string, error) { return "id", nil }

func TestNewSagaOrchestrator_RejectsNilDependencies(t *testing.T) {
	cases := []struct {
		name   string
		g      gen.IGenerator[string]
		logger logging.ILogger
	}{
		{"nil event ID generator", nil, logging.NewNoopLogger()},
		{"typed-nil event ID generator", (*stubSagaStringGenerator)(nil), logging.NewNoopLogger()},
		{"nil logger", gen.NewUUIDGenerator(), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSagaOrchestrator(nil, nil, nil, tc.g, tc.logger)
			require.Error(t, err)
			require.True(t, errors.Is(err, errors.InvalidInput))
		})
	}
}

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

// failingSaga 用于测试失败 + 补偿语义
type failingSaga struct {
	steps           []*SagaStep
	failedCalled    bool
	completedCalled bool
}

// ID 返回当前值。
//
// 返回：
// - result：文本结果
func (s *failingSaga) ID() string { return "saga-fail-1" }

// Steps 返回当前值。
//
// 返回：
// - result：列表结果（元素类型：*SagaStep）
func (s *failingSaga) Steps() []*SagaStep {
	return s.steps
}

// OnComplete ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *failingSaga) OnComplete(ctx context.Context) error {
	s.completedCalled = true
	return nil
}

// OnFailed ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - err：待检查/包装的错误（类型：error）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *failingSaga) OnFailed(ctx context.Context, err error) error {
	s.failedCalled = true
	return nil
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

// TestBaseSaga_DefaultCallbacks_NoError 验证 BaseSaga DefaultCallbacks NoError。
func TestBaseSaga_DefaultCallbacks_NoError(t *testing.T) {
	var b BaseSaga
	require.NoError(t, b.OnComplete(context.Background()))
	require.NoError(t, b.OnFailed(context.Background(), stdErrors.New("x")))
}
