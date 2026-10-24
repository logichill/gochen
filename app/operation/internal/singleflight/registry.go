package singleflight

import (
	"context"
)

// Registry 按 key 跟踪正在执行的调用。
//
// 说明：Registry 内部不加锁；调用方应在 Begin、Get、Finish、Len 外层持有同一把锁，
// 以便把相邻状态检查与 singleflight 注册保持原子。
type Registry[K comparable, V any] struct {
	inflight map[K]*Call[V]
}

// FinishOptions 控制 owner 结果如何保存给等待者。
type FinishOptions[V any] struct {
	Clone       func(V) V
	KeepResult  func(V) bool
	KeepOnError bool
}

// NewRegistry 创建空的进行中调用注册表。
func NewRegistry[K comparable, V any]() Registry[K, V] {
	return Registry[K, V]{inflight: make(map[K]*Call[V])}
}

// Begin 返回 key 当前的进行中调用，或注册一个新的 owner。
func (r *Registry[K, V]) Begin(ctx context.Context, key K) (*Call[V], bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if r.inflight == nil {
		r.inflight = make(map[K]*Call[V])
	}
	if call, ok := r.inflight[key]; ok {
		if !call.closed.Load() {
			return call, false, nil
		}
		delete(r.inflight, key)
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
		// 只移除自己注册的实例：旧 Call 的 Finish 与新 Begin 竞态时，
		// 盲删会把新活跃调用从注册表中清掉，使后续请求无法复用进行中调用，
		// singleflight 退化为并发重复执行。
		if r != nil && r.inflight != nil && r.inflight[key] == call {
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
