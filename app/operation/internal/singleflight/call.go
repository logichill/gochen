package singleflight

import (
	"context"
	"sync/atomic"
)

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

// cloneWaitResult 安全执行等待者侧克隆：panic 转错误，返回原值占位。
func cloneWaitResult[V any](v V, clone func(V) V) (out V, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			out = v
			err = recoveredAsError(recovered)
		}
	}()
	return clone(v), nil
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
			// 克隆函数可能 panic（结果包含不可深拷贝元素）：与 Finish 保持一致，
			// 将 panic 转为错误返回，避免等待者协程连锁崩溃。
			cloned, cloneErr := cloneWaitResult(result, opts.Clone)
			if cloneErr != nil {
				return zero, cloneErr
			}
			result = cloned
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
