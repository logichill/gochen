package eventsourced

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
)

type retryTestAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
}

// newRetryTestAggregate id：对象/实体标识。
//
// 参数：
//
// 返回：
// - result：返回的实例（类型：*retryTestAggregate）
func newRetryTestAggregate(id int64) *retryTestAggregate {
	return &retryTestAggregate{
		EventSourcedAggregate: deventsourced.NewEventSourcedAggregate[int64](id, "RetryTestAggregate"),
	}
}

type retryTestCommand struct {
	id int64
}

// AggregateID 返回聚合标识。
//
// 返回：
// - result：数量/计数
func (c *retryTestCommand) AggregateID() int64 { return c.id }

type flakyRepo struct {
	getOrCreateCalls int
	saveCalls        int
	savedAggs        []*retryTestAggregate
}

// Save ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - aggregate：聚合实例（类型：*retryTestAggregate）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *flakyRepo) Save(ctx context.Context, aggregate *retryTestAggregate) error {
	_ = ctx
	r.saveCalls++
	r.savedAggs = append(r.savedAggs, aggregate)
	if r.saveCalls == 1 {
		return errors.NewCode(errors.Concurrency, "conflict")
	}
	return nil
}

// Get 从存储中查询实体。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：返回结果（类型：*retryTestAggregate）
// - err：错误信息（nil 表示成功）
func (r *flakyRepo) Get(context.Context, int64) (*retryTestAggregate, error) {
	panic("not used")
}

// GetOrCreate 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：*retryTestAggregate）
// - err：错误信息（nil 表示成功）
func (r *flakyRepo) GetOrCreate(ctx context.Context, id int64) (*retryTestAggregate, error) {
	_ = ctx
	r.getOrCreateCalls++
	return newRetryTestAggregate(id), nil
}

// Exists 判断对象是否存在。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：是否满足条件
// - err：错误信息（nil 表示成功）
func (r *flakyRepo) Exists(context.Context, int64) (bool, error) { panic("not used") }

// GetAggregateVersion 从存储中查询实体。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *flakyRepo) GetAggregateVersion(context.Context, int64) (uint64, error) {
	panic("not used")
}

// TestEventSourcedService_ConcurrencyRetry_RerunsHandler 验证 EventSourcedService ConcurrencyRetry RerunsHandler。
func TestEventSourcedService_ConcurrencyRetry_RerunsHandler(t *testing.T) {
	repo := &flakyRepo{}
	hook := &recordingHook{}
	svc, err := NewEventSourcedService[*retryTestAggregate, int64](repo, &EventSourcedServiceOptions[*retryTestAggregate, int64]{
		CommandHooks: []IEventSourcedCommandHook[*retryTestAggregate, int64]{hook},
		ConcurrencyRetry: &RetryConfig{
			MaxRetries:        1,
			InitialBackoff:    0,
			MaxBackoff:        0,
			BackoffMultiplier: 1,
		},
	})
	require.NoError(t, err)

	handlerCalls := 0
	require.NoError(t, svc.RegisterCommandHandler(&retryTestCommand{}, func(ctx context.Context, cmd IEventSourcedCommand[int64], aggregate *retryTestAggregate) error {
		_ = ctx
		_ = cmd
		_ = aggregate
		handlerCalls++
		return nil
	}))

	err = svc.ExecuteCommand(context.Background(), &retryTestCommand{id: 1})
	require.NoError(t, err)
	require.Equal(t, 2, handlerCalls, "handler should be rerun after concurrency conflict")
	require.Equal(t, 2, repo.saveCalls)
	require.Equal(t, 2, repo.getOrCreateCalls)
	require.Len(t, repo.savedAggs, 2)
	require.NotSame(t, repo.savedAggs[0], repo.savedAggs[1], "each attempt should use a newly loaded aggregate")

	require.Equal(t, 2, hook.afterCalls, "AfterExecute should be called once per attempt")
	require.Equal(t, 1, hook.finalCalls, "AfterFinalize should be called once per command execution")
	require.Equal(t, 2, hook.lastFinalAttempts)
	require.Nil(t, hook.lastFinalErr)
}

// TestEventSourcedService_ConcurrencyRetry_DisabledWhenMaxRetriesZero 验证 EventSourcedService ConcurrencyRetry DisabledWhenMaxRetriesZero。
func TestEventSourcedService_ConcurrencyRetry_DisabledWhenMaxRetriesZero(t *testing.T) {
	repo := &flakyRepo{}
	svc, err := NewEventSourcedService[*retryTestAggregate, int64](repo, &EventSourcedServiceOptions[*retryTestAggregate, int64]{
		ConcurrencyRetry: &RetryConfig{
			MaxRetries:        0,
			InitialBackoff:    0,
			MaxBackoff:        0,
			BackoffMultiplier: 1,
		},
	})
	require.NoError(t, err)

	handlerCalls := 0
	require.NoError(t, svc.RegisterCommandHandler(&retryTestCommand{}, func(ctx context.Context, cmd IEventSourcedCommand[int64], aggregate *retryTestAggregate) error {
		_ = ctx
		_ = cmd
		_ = aggregate
		handlerCalls++
		return nil
	}))

	err = svc.ExecuteCommand(context.Background(), &retryTestCommand{id: 1})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Concurrency))
	require.Equal(t, 1, handlerCalls)
	require.Equal(t, 1, repo.saveCalls)
	require.Equal(t, 1, repo.getOrCreateCalls)
}

type alwaysConflictRepoWithSaveSignal struct {
	getOrCreateCalls int
	saveCalls        int
	saveCalled       chan struct{}
}

// Save ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - aggregate：聚合实例（类型：*retryTestAggregate）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *alwaysConflictRepoWithSaveSignal) Save(ctx context.Context, aggregate *retryTestAggregate) error {
	_ = ctx
	_ = aggregate
	r.saveCalls++
	select {
	case r.saveCalled <- struct{}{}:
	default:
	}
	return errors.NewCode(errors.Concurrency, "conflict")
}

// Get 从存储中查询实体。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：返回结果（类型：*retryTestAggregate）
// - err：错误信息（nil 表示成功）
func (r *alwaysConflictRepoWithSaveSignal) Get(context.Context, int64) (*retryTestAggregate, error) {
	panic("not used")
}

// GetOrCreate 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：*retryTestAggregate）
// - err：错误信息（nil 表示成功）
func (r *alwaysConflictRepoWithSaveSignal) GetOrCreate(ctx context.Context, id int64) (*retryTestAggregate, error) {
	_ = ctx
	r.getOrCreateCalls++
	return newRetryTestAggregate(id), nil
}

// Exists 判断对象是否存在。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：是否满足条件
// - err：错误信息（nil 表示成功）
func (r *alwaysConflictRepoWithSaveSignal) Exists(context.Context, int64) (bool, error) {
	panic("not used")
}

// GetAggregateVersion 从存储中查询实体。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *alwaysConflictRepoWithSaveSignal) GetAggregateVersion(context.Context, int64) (uint64, error) {
	panic("not used")
}

// TestEventSourcedService_ConcurrencyRetry_ContextCanceledDuringBackoff 验证 EventSourcedService ConcurrencyRetry ContextCanceledDuringBackoff。
func TestEventSourcedService_ConcurrencyRetry_ContextCanceledDuringBackoff(t *testing.T) {
	repo := &alwaysConflictRepoWithSaveSignal{saveCalled: make(chan struct{}, 1)}
	hook := &recordingHook{}
	svc, err := NewEventSourcedService[*retryTestAggregate, int64](repo, &EventSourcedServiceOptions[*retryTestAggregate, int64]{
		CommandHooks: []IEventSourcedCommandHook[*retryTestAggregate, int64]{hook},
		ConcurrencyRetry: &RetryConfig{
			MaxRetries:        1,
			InitialBackoff:    5 * time.Second,
			MaxBackoff:        5 * time.Second,
			BackoffMultiplier: 1,
			JitterRatio:       0, // keep deterministic for test
		},
	})
	require.NoError(t, err)

	require.NoError(t, svc.RegisterCommandHandler(&retryTestCommand{}, func(ctx context.Context, cmd IEventSourcedCommand[int64], aggregate *retryTestAggregate) error {
		_ = ctx
		_ = cmd
		_ = aggregate
		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- svc.ExecuteCommand(ctx, &retryTestCommand{id: 1})
	}()

	select {
	case <-repo.saveCalled:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for Save to be called")
	}

	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for ExecuteCommand to return after context cancel")
	}

	require.Equal(t, 1, repo.saveCalls)
	require.Equal(t, 1, repo.getOrCreateCalls)

	require.Equal(t, 1, hook.afterCalls)
	require.Equal(t, 1, hook.finalCalls)
	require.Equal(t, 1, hook.lastFinalAttempts)
	require.ErrorIs(t, hook.lastFinalErr, context.Canceled)
}

type alwaysConflictRepo struct {
	getOrCreateCalls int
	saveCalls        int
}

// Save ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - aggregate：聚合实例（类型：*retryTestAggregate）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *alwaysConflictRepo) Save(ctx context.Context, aggregate *retryTestAggregate) error {
	_ = ctx
	_ = aggregate
	r.saveCalls++
	return errors.NewCode(errors.Concurrency, "conflict")
}

// Get 从存储中查询实体。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：返回结果（类型：*retryTestAggregate）
// - err：错误信息（nil 表示成功）
func (r *alwaysConflictRepo) Get(context.Context, int64) (*retryTestAggregate, error) {
	panic("not used")
}

// GetOrCreate 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：*retryTestAggregate）
// - err：错误信息（nil 表示成功）
func (r *alwaysConflictRepo) GetOrCreate(ctx context.Context, id int64) (*retryTestAggregate, error) {
	_ = ctx
	r.getOrCreateCalls++
	return newRetryTestAggregate(id), nil
}

// Exists 判断对象是否存在。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：是否满足条件
// - err：错误信息（nil 表示成功）
func (r *alwaysConflictRepo) Exists(context.Context, int64) (bool, error) { panic("not used") }

// GetAggregateVersion 从存储中查询实体。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *alwaysConflictRepo) GetAggregateVersion(context.Context, int64) (uint64, error) {
	panic("not used")
}

// TestEventSourcedService_ConcurrencyRetry_FinalizeReceivesRetryExhaustedError 验证 EventSourcedService ConcurrencyRetry FinalizeReceivesRetryExhaustedError。
func TestEventSourcedService_ConcurrencyRetry_FinalizeReceivesRetryExhaustedError(t *testing.T) {
	repo := &alwaysConflictRepo{}
	hook := &recordingHook{}
	svc, err := NewEventSourcedService[*retryTestAggregate, int64](repo, &EventSourcedServiceOptions[*retryTestAggregate, int64]{
		CommandHooks: []IEventSourcedCommandHook[*retryTestAggregate, int64]{hook},
		ConcurrencyRetry: &RetryConfig{
			MaxRetries:        1,
			InitialBackoff:    0,
			MaxBackoff:        0,
			BackoffMultiplier: 1,
		},
	})
	require.NoError(t, err)

	require.NoError(t, svc.RegisterCommandHandler(&retryTestCommand{}, func(ctx context.Context, cmd IEventSourcedCommand[int64], aggregate *retryTestAggregate) error {
		_ = ctx
		_ = cmd
		_ = aggregate
		return nil
	}))

	err = svc.ExecuteCommand(context.Background(), &retryTestCommand{id: 1})
	require.Error(t, err)

	var exhausted *RetryExhaustedError
	require.ErrorAs(t, err, &exhausted)

	require.Equal(t, 2, repo.saveCalls)
	require.Equal(t, 2, repo.getOrCreateCalls)

	require.Equal(t, 2, hook.afterCalls)
	require.Equal(t, 1, hook.finalCalls)
	require.Equal(t, 2, hook.lastFinalAttempts)
	require.ErrorAs(t, hook.lastFinalErr, &exhausted)
}
