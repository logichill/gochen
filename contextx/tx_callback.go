package contextx

import (
	"context"
	"sync"

	"gochen/errors"
)

// AfterCommitFunc 定义事务真正提交后的回调。
type AfterCommitFunc func(ctx context.Context) error

// IAfterCommitDispatcher 定义可挂接到事务会话上的提交后回调分发器。
type IAfterCommitDispatcher interface {
	AppendAfterCommit(ctx context.Context, fn AfterCommitFunc) error
	RunAfterCommit() error
}

// AfterCommitDispatcher 是 IAfterCommitDispatcher 的标准并发安全实现。
type AfterCommitDispatcher struct {
	mu      sync.Mutex
	entries []afterCommitEntry
	drained bool
}

type afterCommitEntry struct {
	ctx context.Context
	fn  AfterCommitFunc
}

// AfterCommitError 表示“数据库已提交，但提交后回调失败”。
type AfterCommitError struct {
	cause error
}

type afterCommitDispatcherKey struct{}

type txLifecycleState struct {
	mu        sync.Mutex
	callbacks []AfterCommitFunc
	drained   bool
}

type txLifecycleStateKey struct{}
type txLifecycleOwnerKey struct{}

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

func afterCommitDispatcherFromContext(ctx context.Context) (IAfterCommitDispatcher, bool) {
	if ctx == nil {
		return nil, false
	}
	dispatcher, ok := ctx.Value(afterCommitDispatcherKey{}).(IAfterCommitDispatcher)
	if !ok || dispatcher == nil {
		return nil, false
	}
	return dispatcher, true
}

// NewAfterCommitDispatcher 创建一个可复用的提交后回调分发器。
func NewAfterCommitDispatcher() *AfterCommitDispatcher {
	return &AfterCommitDispatcher{}
}

// Error 实现 error 接口。
func (e *AfterCommitError) Error() string {
	if e == nil || e.cause == nil {
		return "after-commit callback failed"
	}
	return "after-commit callback failed: " + e.cause.Error()
}

func (e *AfterCommitError) Unwrap() error { return e.cause }

// WrapAfterCommitError 把提交后回调错误标记为“提交已成功”。
func WrapAfterCommitError(err error) error {
	if err == nil || IsAfterCommitError(err) {
		return err
	}
	return &AfterCommitError{cause: err}
}

// IsAfterCommitError 判断 err 是否表示“提交成功但回调失败”。
func IsAfterCommitError(err error) bool {
	var target *AfterCommitError
	return errors.As(err, &target)
}

// WithAfterCommitDispatcher 将外部事务回调分发器写入 ctx。
func WithAfterCommitDispatcher(ctx context.Context, dispatcher IAfterCommitDispatcher) (context.Context, error) {
	ctx, err := Ensure(ctx)
	if err != nil {
		return nil, err
	}
	if dispatcher == nil {
		return nil, errors.NewCode(errors.InvalidInput, "after commit dispatcher is nil")
	}
	return context.WithValue(ctx, afterCommitDispatcherKey{}, dispatcher), nil
}

func (d *AfterCommitDispatcher) AppendAfterCommit(ctx context.Context, fn AfterCommitFunc) error {
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "after commit callback is nil")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.drained {
		return errors.NewCode(errors.InvalidInput, "after commit callbacks already executed")
	}
	d.entries = append(d.entries, afterCommitEntry{ctx: ctx, fn: fn})
	return nil
}

// RunAfterCommit 执行全部已注册回调，并合并返回所有失败。
func (d *AfterCommitDispatcher) RunAfterCommit() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	if d.drained {
		d.mu.Unlock()
		return nil
	}
	entries := append([]afterCommitEntry(nil), d.entries...)
	d.entries = nil
	d.drained = true
	d.mu.Unlock()

	callbackErrs := make([]error, 0)
	for _, entry := range entries {
		if entry.fn == nil {
			continue
		}
		if err := entry.fn(entry.ctx); err != nil {
			callbackErrs = append(callbackErrs, err)
		}
	}
	return errors.Join(callbackErrs...)
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

	ctx = context.WithValue(ctx, txLifecycleStateKey{}, state)
	ctx = context.WithValue(ctx, txLifecycleOwnerKey{}, owned)
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
	state.callbacks = append(state.callbacks, fn)
	return nil
}

// RunAfterCommit 在最外层事务成功提交后执行所有回调，并合并返回所有失败。
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
		if err := fn(ctx); err != nil {
			callbackErrs = append(callbackErrs, err)
		}
	}
	return errors.Join(callbackErrs...)
}
