package contextx

import (
	stdctx "context"
	"gochen/errors"
)

// AfterCommitFunc 定义事务真正提交后的回调。
type AfterCommitFunc func(ctx stdctx.Context) error

// IAfterCommitDispatcher 定义可挂接到事务会话上的提交后回调分发器。
type IAfterCommitDispatcher interface {
	AppendAfterCommit(ctx stdctx.Context, fn AfterCommitFunc) error
	RunAfterCommit() error
}

type afterCommitDispatcher struct {
	entries []afterCommitEntry
	drained bool
}

type afterCommitEntry struct {
	ctx stdctx.Context
	fn  AfterCommitFunc
}

// AfterCommitError 表示“数据库已提交，但提交后回调失败”。
type AfterCommitError struct {
	cause error
}

type afterCommitDispatcherKey struct{}

type txLifecycleState struct {
	callbacks []AfterCommitFunc
	drained   bool
}

type txLifecycleStateKey struct{}
type txLifecycleOwnerKey struct{}

func txLifecycleStateFromContext(ctx stdctx.Context) (*txLifecycleState, bool, bool) {
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

func afterCommitDispatcherFromContext(ctx stdctx.Context) (IAfterCommitDispatcher, bool) {
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
func NewAfterCommitDispatcher() IAfterCommitDispatcher {
	return &afterCommitDispatcher{}
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
func WithAfterCommitDispatcher(ctx stdctx.Context, dispatcher IAfterCommitDispatcher) (stdctx.Context, error) {
	ctx, err := Ensure(ctx)
	if err != nil {
		return nil, err
	}
	if dispatcher == nil {
		return nil, errors.NewCode(errors.InvalidInput, "after commit dispatcher is nil")
	}
	return stdctx.WithValue(ctx, afterCommitDispatcherKey{}, dispatcher), nil
}

func (d *afterCommitDispatcher) AppendAfterCommit(ctx stdctx.Context, fn AfterCommitFunc) error {
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "after commit callback is nil")
	}
	if d.drained {
		return errors.NewCode(errors.InvalidInput, "after commit callbacks already executed")
	}
	d.entries = append(d.entries, afterCommitEntry{ctx: ctx, fn: fn})
	return nil
}

func (d *afterCommitDispatcher) RunAfterCommit() error {
	if d == nil || d.drained {
		return nil
	}
	entries := append([]afterCommitEntry(nil), d.entries...)
	d.entries = nil
	d.drained = true
	for _, entry := range entries {
		if err := entry.fn(entry.ctx); err != nil {
			return err
		}
	}
	return nil
}

// WithTxLifecycle 为当前事务链路绑定共享的 after-commit 状态与 owner 标记。
func WithTxLifecycle(ctx stdctx.Context, owned bool) (stdctx.Context, error) {
	ctx, err := Ensure(ctx)
	if err != nil {
		return nil, err
	}

	state, _, ok := txLifecycleStateFromContext(ctx)
	if !ok {
		state = &txLifecycleState{}
	}

	ctx = stdctx.WithValue(ctx, txLifecycleStateKey{}, state)
	ctx = stdctx.WithValue(ctx, txLifecycleOwnerKey{}, owned)
	return ctx, nil
}

// TxLifecycleFromContext 返回当前 ctx 的事务 owner 标记。
func TxLifecycleFromContext(ctx stdctx.Context) (owned bool, ok bool) {
	_, owned, ok = txLifecycleStateFromContext(ctx)
	return owned, ok
}

// AppendAfterCommit 向当前事务链路追加真正提交后的回调。
func AppendAfterCommit(ctx stdctx.Context, fn AfterCommitFunc) error {
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
	if state.drained {
		return errors.NewCode(errors.InvalidInput, "after commit callbacks already executed")
	}
	state.callbacks = append(state.callbacks, fn)
	return nil
}

// RunAfterCommit 在最外层事务成功提交后执行所有回调。
func RunAfterCommit(ctx stdctx.Context) error {
	state, owned, ok := txLifecycleStateFromContext(ctx)
	if !ok || !owned || state.drained {
		return nil
	}

	callbacks := append([]AfterCommitFunc(nil), state.callbacks...)
	state.callbacks = nil
	state.drained = true

	for _, fn := range callbacks {
		if err := fn(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ITxLifecycleRunner 提供标准事务生命周期 primitive。
type ITxLifecycleRunner interface {
	BeginTx(ctx stdctx.Context) (TxScope, error)
	Commit(tx TxScope) error
	Rollback(tx TxScope) error
}

// RunTxLifecycle 统一执行 begin -> fn -> commit -> after-commit，失败时自动 rollback。
func RunTxLifecycle(ctx stdctx.Context, runner ITxLifecycleRunner, fn func(txCtx stdctx.Context) error) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if runner == nil {
		return errors.NewCode(errors.InvalidInput, "tx lifecycle runner is nil")
	}
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "fn is nil")
	}

	txScope, err := runner.BeginTx(ctx)
	if err != nil {
		return err
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		_ = runner.Rollback(txScope)
	}()

	txCtx := txScope.Context()
	if txCtx == nil {
		return errors.NewCode(errors.Internal, "BeginTx returned nil txCtx")
	}
	owned, ok := TxLifecycleFromContext(txCtx)
	if !ok {
		return errors.NewCode(errors.Internal, "BeginTx returned txCtx without transaction lifecycle metadata")
	}
	if owned != txScope.Owned() {
		return errors.NewCode(errors.Internal, "BeginTx returned mismatched transaction owner metadata")
	}

	if err := fn(txCtx); err != nil {
		return err
	}
	if err := runner.Commit(txScope); err != nil {
		if IsAfterCommitError(err) {
			committed = true
		}
		return err
	}

	committed = true
	if err := RunAfterCommit(txCtx); err != nil {
		return WrapAfterCommitError(err)
	}
	return nil
}
