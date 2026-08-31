// Package singleflight 提供进程内同 key 单 owner 执行原语。
package singleflight

import (
	"context"
	"fmt"
	"sync/atomic"
)

// Registry 按 key 跟踪正在执行的调用。
//
// 说明：Registry 内部不加锁；调用方应在 Begin、Get、Finish、Len 外层持有同一把锁，
// 以便把相邻状态检查与 singleflight 注册保持原子。
type Registry[K comparable, V any] struct {
	inflight map[K]*Call[V]
}

// Call 表示一个正在执行的 owner 调用及其等待者。
type Call[V any] struct {
	done   chan struct{}
	result V
	err    error
	closed atomic.Bool
}

// WaitOptions 控制等待者如何接收 owner 结果。
type WaitOptions[V any] struct {
	Clone         func(V) V
	HasResult     func(V) bool
	NoResultError func() error
}

// FinishOptions 控制 owner 结果如何保存给等待者。
type FinishOptions[V any] struct {
	Clone       func(V) V
	KeepResult  func(V) bool
	KeepOnError bool
}

type finishPanicError struct {
	value any
}

func (e finishPanicError) Error() string {
	return fmt.Sprintf("singleflight finish panic: %v", e.value)
}

func recoveredAsError(value any) error {
	if err, ok := value.(error); ok {
		return err
	}
	return finishPanicError{value: value}
}

func safeFinish[V any](call *Call[V], result V, err error, finish func(*Call[V], V, error)) {
	if finish == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil && call != nil && call.closed.CompareAndSwap(false, true) {
			call.err = recoveredAsError(recovered)
			if call.done != nil {
				close(call.done)
			}
		}
	}()
	finish(call, result, err)
}

// NewRegistry 创建空的进行中调用注册表。
func NewRegistry[K comparable, V any]() Registry[K, V] {
	return Registry[K, V]{inflight: make(map[K]*Call[V])}
}

// Begin 返回 key 当前的进行中调用，或注册一个新的 owner。
func (r *Registry[K, V]) Begin(ctx context.Context, key K) (*Call[V], bool, error) {
	if r.inflight == nil {
		r.inflight = make(map[K]*Call[V])
	}
	if call, ok := r.inflight[key]; ok {
		if !call.closed.Load() {
			return call, false, nil
		}
		delete(r.inflight, key)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	call := &Call[V]{done: make(chan struct{})}
	r.inflight[key] = call
	return call, true, nil
}

// Get 返回 key 当前的进行中调用。
func (r *Registry[K, V]) Get(key K) (*Call[V], bool) {
	if r == nil || r.inflight == nil {
		return nil, false
	}
	call, ok := r.inflight[key]
	return call, ok
}

// Len 返回当前进行中调用数量。
func (r *Registry[K, V]) Len() int {
	if r == nil || r.inflight == nil {
		return 0
	}
	return len(r.inflight)
}

// Finish 发布 owner 结果、移除 key 并唤醒等待者。
func (r *Registry[K, V]) Finish(key K, call *Call[V], result V, err error, opts FinishOptions[V]) bool {
	if call == nil || !call.closed.CompareAndSwap(false, true) {
		return false
	}
	defer func() {
		if r != nil && r.inflight != nil {
			delete(r.inflight, key)
		}
		if call.done != nil {
			close(call.done)
		}
	}()

	call.err = err
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				call.err = recoveredAsError(recovered)
			}
		}()
		if err == nil || opts.KeepOnError {
			keep := true
			if opts.KeepResult != nil {
				keep = opts.KeepResult(result)
			}
			if keep {
				if opts.Clone != nil {
					result = opts.Clone(result)
				}
				call.result = result
			}
		}
	}()
	return true
}

// Closed 报告 Finish 是否已经调用。
func (c *Call[V]) Closed() bool {
	return c == nil || c.closed.Load()
}

// Wait 阻塞直到 owner 完成或 ctx 取消。
func (c *Call[V]) Wait(ctx context.Context, opts WaitOptions[V]) (V, error) {
	var zero V
	if c == nil {
		if opts.NoResultError != nil {
			return zero, opts.NoResultError()
		}
		return zero, nil
	}
	select {
	case <-c.done:
		result := c.result
		if opts.Clone != nil {
			result = opts.Clone(result)
		}
		if c.err != nil {
			if opts.HasResult != nil && opts.HasResult(result) {
				return result, c.err
			}
			return zero, c.err
		}
		if opts.NoResultError != nil && opts.HasResult != nil && !opts.HasResult(result) {
			return zero, opts.NoResultError()
		}
		return result, nil
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// Run 为 owner 执行 fn，或等待已有 owner 调用完成。
func Run[V any](
	ctx context.Context,
	call *Call[V],
	owner bool,
	wait func(context.Context, *Call[V]) (V, error),
	finish func(*Call[V], V, error),
	panicError func(any) error,
	fn func() (V, error),
) (V, error) {
	var zero V
	if !owner {
		if wait == nil {
			return zero, nil
		}
		return wait(ctx, call)
	}

	var (
		result  V
		execErr error
	)
	defer func() {
		if recovered := recover(); recovered != nil {
			var err error
			if panicError != nil {
				err = panicError(recovered)
			}
			safeFinish(call, zero, err, finish)
			panic(recovered)
		}
		safeFinish(call, result, execErr, finish)
	}()
	result, execErr = fn()
	if execErr != nil {
		return result, execErr
	}
	return result, nil
}
