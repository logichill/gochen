package contextx

import (
	"context"
	"sync"

	"gochen/errors"
)

// IAfterCommitDispatcher 定义可挂接到事务会话上的提交后回调分发器。
type IAfterCommitDispatcher interface {
	AppendAfterCommit(ctx context.Context, fn AfterCommitFunc) error
	RunAfterCommit() error
}

// IAfterCommitMarkableDispatcher 为可嵌套回滚的分发器提供回调水位线能力。
//
// AppendAfterCommit 会优先写入上下文中的分发器而非 state.callbacks，因此
// 嵌套内层回滚必须能按水位线截断分发器中的条目，否则内层回调会残留并在
// 外层提交后错误执行。
type IAfterCommitMarkableDispatcher interface {
	// MarkAfterCommit 返回当前回调条目数作为进入本层作用域的水位线。
	MarkAfterCommit() int
	// TruncateAfterCommit 截断到指定水位线，撤销该点之后注册的回调。
	TruncateAfterCommit(mark int)
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

type afterCommitDispatcherKey struct{}

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

// MarkAfterCommit 返回当前回调条目数作为嵌套作用域水位线。
func (d *AfterCommitDispatcher) MarkAfterCommit() int {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.entries)
}

// TruncateAfterCommit 撤销水位线之后注册的回调，保留外层回调。
func (d *AfterCommitDispatcher) TruncateAfterCommit(mark int) {
	if d == nil {
		return
	}
	if mark < 0 {
		mark = 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if mark < len(d.entries) {
		d.entries = d.entries[:mark]
	}
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
		// 单个回调 panic 不得终止后续回调，也不得将 panic 泄漏到事务 runner；
		// 转换为内部错误与其它回调失败合并上报。
		if err := runAfterCommitCallbackSafely(entry.ctx, entry.fn); err != nil {
			callbackErrs = append(callbackErrs, err)
		}
	}
	return errors.Join(callbackErrs...)
}

// Reset 清空全部未执行的回调并复位 drained 状态，使分发器可被重新使用。
//
// 典型场景是事务回滚/清理路径：回调永远不会被判发，若不重置，残留条目中
// 捕获的 context 引用无法释放（内存泄漏），且 drained/entries 残留会让复用
// 的会话在下一次事务中报错或行为错乱。复用方应在每次新事务开始前 Reset。
func (d *AfterCommitDispatcher) Reset() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.entries = nil
	d.drained = false
	d.mu.Unlock()
}
