package contextx

import (
	"context"
	"fmt"
	"sync"

	"gochen/errors"
)

type txLifecycleDispatcherMarkKey struct{}

// AfterCommitFunc 定义事务真正提交后的回调。
type AfterCommitFunc func(ctx context.Context) error

type txLifecycleState struct {
	mu        sync.Mutex
	callbacks []AfterCommitFunc
	// committed 标记主机事务（最外层）是否已成功提交。
	// 提交后进入“锁定期”：拒绝再追加回调，并封存当前回调批次，
	// 避免提交与派发之间新注册的回调被静默遗漏或提前派发。
	committed bool
	drained   bool
}

type txLifecycleStateKey struct{}

type txLifecycleOwnerKey struct{}

// txLifecycleMarkKey 记录进入某层作用域时回调切片的长度。
//
// 嵌套内层回滚时，只有「进入本层之后新增」的回调需要撤销；外层在进入本层
// 之前注册的回调必须保留。仅凭 owner=false 无法区分二者，因此需要在进入
// 作用域时留下水位线。
type txLifecycleMarkKey struct{}

type withoutTxLifecycleContext struct {
	context.Context
}

func (c withoutTxLifecycleContext) Value(key any) any {
	switch key.(type) {
	case txLifecycleStateKey, txLifecycleOwnerKey, txLifecycleMarkKey, txLifecycleDispatcherMarkKey, afterCommitDispatcherKey:
		return nil
	default:
		return c.Context.Value(key)
	}
}

// WithoutTxLifecycle 派生不携带事务生命周期或提交后分发器的上下文。
// 保留其他值、截止时间和取消信号；不提交、回滚或修改原事务状态。
// 底层数据库事务句柄应由其所属包一并移除。
func WithoutTxLifecycle(ctx context.Context) (context.Context, error) {
	if _, err := Ensure(ctx); err != nil {
		return nil, err
	}
	return withoutTxLifecycleContext{Context: ctx}, nil
}

func txLifecycleStateFromContext(ctx context.Context) (*txLifecycleState, bool, bool) {
	if ctx == nil {
		return nil, false, false
	}
	state, ok := ctx.Value(txLifecycleStateKey{}).(*txLifecycleState)
	if !ok || state == nil {
		return nil, false, false
	}
	owned, _ := ctx.Value(txLifecycleOwnerKey{}).(bool)
	return state, owned, true
}

// txLifecycleMarkFromContext 返回进入本层作用域时的回调水位线。
func txLifecycleMarkFromContext(ctx context.Context) (int, bool) {
	if ctx == nil {
		return 0, false
	}
	mark, ok := ctx.Value(txLifecycleMarkKey{}).(int)
	return mark, ok
}

// dispatcherMarkFromContext 返回进入本层作用域时分发器的回调水位线。
func dispatcherMarkFromContext(ctx context.Context) (int, bool) {
	if ctx == nil {
		return 0, false
	}
	mark, ok := ctx.Value(txLifecycleDispatcherMarkKey{}).(int)
	return mark, ok
}

// WithAfterCommitMark 记录分发器回调水位线，供嵌套内层回滚截断使用。
//
// 由负责注入 IAfterCommitDispatcher 的装配层（如 ORM WithTxSession）在每次
// 进入新作用域时调用，使 ResetAfterCommit 能区分「本层新增」与「外层已有」。
func WithAfterCommitMark(ctx context.Context, mark int) (context.Context, error) {
	ctx, err := Ensure(ctx)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, txLifecycleDispatcherMarkKey{}, mark), nil
}

// ResetAfterCommit 清空当前事务链路上未执行的提交后回调，并按 owner 语义封存状态。
//
// 用于事务回滚/清理路径：回调永远不会被判发，不清理则回调及其捕获的
// context 引用常驻内存；若事务对象被复用，残留回调还可能在下一事务提交后
// 误触发。无事务生命周期时静默返回。
//
// owned=true（最外层事务回滚）：整个主机事务作废，封存状态；后续再注册回调
// 属于对已失败事务的操作，必须报错而不是静默接受。
//
// owned=false（嵌套内层作用域回滚）：只撤销内层注册的回调，保留外层回调，
// 使外层可以自行决定继续提交或一并回滚；不设置 committed 锁定，否则会把
// 下层 rollback-only 语义错误地传播到外层。
func ResetAfterCommit(ctx context.Context) {
	state, owned, ok := txLifecycleStateFromContext(ctx)
	if !ok {
		return
	}
	state.mu.Lock()
	if owned {
		state.callbacks = nil
		state.drained = true
	} else if mark, hasMark := txLifecycleMarkFromContext(ctx); hasMark && mark < len(state.callbacks) {
		// 只截断到进入本层时的水位线：外层在进入本层前注册的回调保留。
		state.callbacks = state.callbacks[:mark]
	}
	state.mu.Unlock()

	// AppendAfterCommit 会优先写入上下文中的分发器（如 ORM Session），
	// 此路回调不在 state.callbacks 中，必须一并清理。
	if dispatcher, hasDispatcher := afterCommitDispatcherFromContext(ctx); hasDispatcher {
		if owned {
			if resetter, ok := dispatcher.(interface{ Reset() }); ok {
				resetter.Reset()
			}
			return
		}
		if mark, hasMark := dispatcherMarkFromContext(ctx); hasMark {
			if truncater, ok := dispatcher.(IAfterCommitMarkableDispatcher); ok {
				truncater.TruncateAfterCommit(mark)
			}
		}
	}
}

// WithTxLifecycle 为当前事务链路绑定共享的 after-commit 状态与 owner 标记。
func WithTxLifecycle(ctx context.Context, owned bool) (context.Context, error) {
	ctx, err := Ensure(ctx)
	if err != nil {
		return nil, err
	}

	state, _, ok := txLifecycleStateFromContext(ctx)
	if !ok {
		state = &txLifecycleState{}
	}

	// 记录进入本层时的回调水位线：内层回滚只需截断到此处。
	state.mu.Lock()
	mark := len(state.callbacks)
	state.mu.Unlock()

	ctx = context.WithValue(ctx, txLifecycleStateKey{}, state)
	ctx = context.WithValue(ctx, txLifecycleOwnerKey{}, owned)
	ctx = context.WithValue(ctx, txLifecycleMarkKey{}, mark)
	// 分发器路径同样记录水位线（AppendAfterCommit 会写到那里）。
	if dispatcher, hasDispatcher := afterCommitDispatcherFromContext(ctx); hasDispatcher {
		if marker, ok := dispatcher.(IAfterCommitMarkableDispatcher); ok {
			ctx = context.WithValue(ctx, txLifecycleDispatcherMarkKey{}, marker.MarkAfterCommit())
		}
	}
	return ctx, nil
}

// TxLifecycleFromContext 返回当前 ctx 的事务 owner 标记。
func TxLifecycleFromContext(ctx context.Context) (owned bool, ok bool) {
	_, owned, ok = txLifecycleStateFromContext(ctx)
	return owned, ok
}

// AppendAfterCommit 向当前事务链路追加真正提交后的回调。
func AppendAfterCommit(ctx context.Context, fn AfterCommitFunc) error {
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "after commit callback is nil")
	}
	if dispatcher, ok := afterCommitDispatcherFromContext(ctx); ok {
		return dispatcher.AppendAfterCommit(ctx, fn)
	}
	state, _, ok := txLifecycleStateFromContext(ctx)
	if !ok {
		return errors.NewCode(errors.InvalidInput, "transaction lifecycle not started")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.drained {
		return errors.NewCode(errors.InvalidInput, "after commit callbacks already executed")
	}
	if state.committed {
		// 主机事务已提交，回调批次已封存。再追加只会在该批次之后写入，
		// RunAfterCommit 不会看到它；静默接受等于丢失回调。
		return errors.NewCode(errors.FailedPrecondition,
			"transaction already committed; after commit callback would be lost")
	}
	state.callbacks = append(state.callbacks, fn)
	return nil
}

// MarkTxCommitted 封存当前事务链路的提交后回调批次。
//
// 由负责提交的 TxScope 在底层事务提交成功后调用，使 AppendAfterCommit
// 拒绝在此之后写入（否则回调会落入已封存批次之外被静默丢弃）。
func MarkTxCommitted(ctx context.Context) {
	state, _, ok := txLifecycleStateFromContext(ctx)
	if !ok {
		return
	}
	state.mu.Lock()
	state.committed = true
	state.mu.Unlock()
}

// RunAfterCommit 在最外层事务成功提交后执行所有回调，并合并返回所有失败。
// 与 AppendAfterCommit 的路由语义对称：若上下文中注入了 IAfterCommitDispatcher
// （如 ORM Session），则一并触发调度，避免提交后回调被静默丢弃。
func RunAfterCommit(ctx context.Context) error {
	state, owned, ok := txLifecycleStateFromContext(ctx)
	if !ok || !owned {
		return nil
	}

	state.mu.Lock()
	if state.drained {
		state.mu.Unlock()
		return nil
	}
	callbacks := append([]AfterCommitFunc(nil), state.callbacks...)
	state.callbacks = nil
	state.drained = true
	state.mu.Unlock()

	callbackErrs := make([]error, 0)
	for _, fn := range callbacks {
		if fn == nil {
			continue
		}
		// 单个回调 panic 不得终止后续回调，也不得将 panic 泄漏到事务 runner：
		// 转换为内部错误与其它回调失败合并上报。
		if err := runAfterCommitCallbackSafely(ctx, fn); err != nil {
			callbackErrs = append(callbackErrs, err)
		}
	}
	if dispatcher, hasDispatcher := afterCommitDispatcherFromContext(ctx); hasDispatcher {
		if err := dispatcher.RunAfterCommit(); err != nil {
			callbackErrs = append(callbackErrs, err)
		}
	}
	return errors.Join(callbackErrs...)
}

// runAfterCommitCallbackSafely 执行单个提交后回调并拦截 panic 转为错误。
func runAfterCommitCallbackSafely(ctx context.Context, fn AfterCommitFunc) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.NewCode(errors.Internal, "after commit callback panicked").
				WithContext("panic", fmt.Sprint(r))
		}
	}()
	return fn(ctx)
}
