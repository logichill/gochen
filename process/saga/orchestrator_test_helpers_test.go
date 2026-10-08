package saga

import (
	"context"

	"testing"
	"time"

	"gochen/testkit/assert"
	"gochen/testkit/require"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/gen"
	"gochen/messaging"
	"gochen/messaging/command"

	"gochen/observe/logging"
	"gochen/process/lock"
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

type stubSagaStringGenerator struct{}

func (s *stubSagaStringGenerator) Next() (string, error) { return "id", nil }

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
