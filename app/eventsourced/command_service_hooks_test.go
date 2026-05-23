package eventsourced

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/errors"
)

type recordingHook struct {
	beforeCalls int
	afterCalls  int
	finalCalls  int

	lastAgg *retryTestAggregate
	lastErr error

	lastFinalAgg      *retryTestAggregate
	lastFinalErr      error
	lastFinalAttempts int
}

// BeforeExecute ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - cmd：命令（待处理的输入）（类型：IEventSourcedCommand[int64]）
// - agg：聚合实例（类型：*retryTestAggregate）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h *recordingHook) BeforeExecute(ctx context.Context, cmd IEventSourcedCommand[int64], agg *retryTestAggregate) error {
	_ = ctx
	_ = cmd
	_ = agg
	h.beforeCalls++
	return nil
}

// AfterExecute ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - cmd：命令（待处理的输入）（类型：IEventSourcedCommand[int64]）
// - agg：聚合实例（类型：*retryTestAggregate）
// - err：待检查/包装的错误（类型：error）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h *recordingHook) AfterExecute(ctx context.Context, cmd IEventSourcedCommand[int64], agg *retryTestAggregate, err error) error {
	_ = ctx
	_ = cmd
	h.afterCalls++
	h.lastAgg = agg
	h.lastErr = err
	return nil
}

// AfterFinalize ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - cmd：命令（待处理的输入）（类型：IEventSourcedCommand[int64]）
// - agg：聚合实例（类型：*retryTestAggregate）
// - err：待检查/包装的错误（类型：error）
// - attempts：参数值（具体语义见函数上下文）（类型：int）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h *recordingHook) AfterFinalize(ctx context.Context, cmd IEventSourcedCommand[int64], agg *retryTestAggregate, err error, attempts int) error {
	_ = ctx
	_ = cmd
	h.finalCalls++
	h.lastFinalAgg = agg
	h.lastFinalErr = err
	h.lastFinalAttempts = attempts
	return nil
}

type getOrCreateFailRepo struct{}

// Save context.Context：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - obj：绑定目标（通常为指针）（类型：*retryTestAggregate）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *getOrCreateFailRepo) Save(context.Context, *retryTestAggregate) error { panic("not used") }

// Get 从存储中查询实体。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：返回结果（类型：*retryTestAggregate）
// - err：错误信息（nil 表示成功）
func (r *getOrCreateFailRepo) Get(context.Context, int64) (*retryTestAggregate, error) {
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
func (r *getOrCreateFailRepo) GetOrCreate(ctx context.Context, id int64) (*retryTestAggregate, error) {
	_ = ctx
	_ = id
	return nil, errors.NewCode(errors.Dependency, "load failed")
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
func (r *getOrCreateFailRepo) Exists(context.Context, int64) (bool, error) { panic("not used") }

// GetAggregateVersion 从存储中查询实体。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *getOrCreateFailRepo) GetAggregateVersion(context.Context, int64) (uint64, error) {
	panic("not used")
}

// TestEventSourcedService_AfterExecute_CalledOnLoadFailure 验证 EventSourcedService AfterExecute CalledOnLoadFailure。
func TestEventSourcedService_AfterExecute_CalledOnLoadFailure(t *testing.T) {
	hook := &recordingHook{}
	svc, err := NewEventSourcedService[*retryTestAggregate, int64](&getOrCreateFailRepo{}, &EventSourcedServiceOptions[*retryTestAggregate, int64]{
		CommandHooks: []IEventSourcedCommandHook[*retryTestAggregate, int64]{hook},
	})
	require.NoError(t, err)

	require.NoError(t, svc.RegisterCommandHandler(&retryTestCommand{}, func(ctx context.Context, cmd IEventSourcedCommand[int64], aggregate *retryTestAggregate) error {
		t.Fatalf("handler should not be called when load fails")
		return nil
	}))

	execErr := svc.ExecuteCommand(context.Background(), &retryTestCommand{id: 1})
	require.Error(t, execErr)

	require.Equal(t, 0, hook.beforeCalls)
	require.Equal(t, 1, hook.afterCalls)
	require.Nil(t, hook.lastAgg)
	require.NotNil(t, hook.lastErr)
	require.True(t, errors.Is(hook.lastErr, errors.Dependency))

	require.Equal(t, 1, hook.finalCalls)
	require.Nil(t, hook.lastFinalAgg)
	require.NotNil(t, hook.lastFinalErr)
	require.True(t, errors.Is(hook.lastFinalErr, errors.Dependency))
	require.Equal(t, 1, hook.lastFinalAttempts)
}

type failingBeforeHook struct {
	beforeCalls  int
	afterCalls   int
	finalCalls   int
	lastErr      error
	lastFinalErr error
	lastAttempts int
}

// BeforeExecute ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - cmd：命令（待处理的输入）（类型：IEventSourcedCommand[int64]）
// - agg：聚合实例（类型：*retryTestAggregate）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h *failingBeforeHook) BeforeExecute(ctx context.Context, cmd IEventSourcedCommand[int64], agg *retryTestAggregate) error {
	_ = ctx
	_ = cmd
	_ = agg
	h.beforeCalls++
	return errors.NewCode(errors.Validation, "rejected")
}

// AfterExecute ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - cmd：命令（待处理的输入）（类型：IEventSourcedCommand[int64]）
// - agg：聚合实例（类型：*retryTestAggregate）
// - err：待检查/包装的错误（类型：error）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h *failingBeforeHook) AfterExecute(ctx context.Context, cmd IEventSourcedCommand[int64], agg *retryTestAggregate, err error) error {
	_ = ctx
	_ = cmd
	_ = agg
	h.afterCalls++
	h.lastErr = err
	return nil
}

// AfterFinalize ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - cmd：命令（待处理的输入）（类型：IEventSourcedCommand[int64]）
// - agg：聚合实例（类型：*retryTestAggregate）
// - err：待检查/包装的错误（类型：error）
// - attempts：参数值（具体语义见函数上下文）（类型：int）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (h *failingBeforeHook) AfterFinalize(ctx context.Context, cmd IEventSourcedCommand[int64], agg *retryTestAggregate, err error, attempts int) error {
	_ = ctx
	_ = cmd
	_ = agg
	h.finalCalls++
	h.lastFinalErr = err
	h.lastAttempts = attempts
	return nil
}

type okRepo struct {
	saveCalls int
}

// Save ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - aggregate：聚合实例（类型：*retryTestAggregate）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *okRepo) Save(ctx context.Context, aggregate *retryTestAggregate) error {
	_ = ctx
	_ = aggregate
	r.saveCalls++
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
func (r *okRepo) Get(context.Context, int64) (*retryTestAggregate, error) { panic("not used") }

// GetOrCreate 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：*retryTestAggregate）
// - err：错误信息（nil 表示成功）
func (r *okRepo) GetOrCreate(ctx context.Context, id int64) (*retryTestAggregate, error) {
	_ = ctx
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
func (r *okRepo) Exists(context.Context, int64) (bool, error) { panic("not used") }

// GetAggregateVersion 从存储中查询实体。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：int64）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *okRepo) GetAggregateVersion(context.Context, int64) (uint64, error) { panic("not used") }

// TestEventSourcedService_AfterExecute_CalledWhenBeforeHookFails 验证 EventSourcedService AfterExecute CalledWhenBeforeHookFails。
func TestEventSourcedService_AfterExecute_CalledWhenBeforeHookFails(t *testing.T) {
	repo := &okRepo{}
	beforeFailHook := &failingBeforeHook{}
	lateHook := &recordingHook{}
	svc, err := NewEventSourcedService[*retryTestAggregate, int64](repo, &EventSourcedServiceOptions[*retryTestAggregate, int64]{
		CommandHooks: []IEventSourcedCommandHook[*retryTestAggregate, int64]{beforeFailHook, lateHook},
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

	execErr := svc.ExecuteCommand(context.Background(), &retryTestCommand{id: 1})
	require.Error(t, execErr)
	require.True(t, errors.Is(execErr, errors.Validation))

	require.Equal(t, 1, beforeFailHook.beforeCalls)
	require.Equal(t, 1, beforeFailHook.afterCalls)
	require.True(t, errors.Is(beforeFailHook.lastErr, errors.Validation))
	require.Equal(t, 1, beforeFailHook.finalCalls)
	require.True(t, errors.Is(beforeFailHook.lastFinalErr, errors.Validation))
	require.Equal(t, 1, beforeFailHook.lastAttempts)

	require.Equal(t, 0, lateHook.beforeCalls, "hooks after a failing BeforeExecute are not executed")
	require.Equal(t, 1, lateHook.afterCalls, "but they still receive AfterExecute in option C semantics")
	require.NotNil(t, lateHook.lastAgg)
	require.True(t, errors.Is(lateHook.lastErr, errors.Validation))
	require.Equal(t, 1, lateHook.finalCalls)
	require.NotNil(t, lateHook.lastFinalAgg)
	require.True(t, errors.Is(lateHook.lastFinalErr, errors.Validation))
	require.Equal(t, 1, lateHook.lastFinalAttempts)

	require.Equal(t, 0, handlerCalls)
	require.Equal(t, 0, repo.saveCalls)
}
