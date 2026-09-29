// Package cleanup 提供框架内部的可重试资源释放控制。
package cleanup

import (
	"context"
	"sync/atomic"

	"gochen/errors"
)

// Retryable 串行执行 release，仅在成功后记为完成；等待其他释放调用时可取消。
// release 必须非 nil，且应遵循传入的 context。返回函数可并发调用。
func Retryable(release func(context.Context) error) func(context.Context) error {
	gate := make(chan struct{}, 1)
	var completed atomic.Bool
	return func(ctx context.Context) error {
		if ctx == nil {
			return errors.NewCode(errors.InvalidInput, "ctx is nil")
		}
		if completed.Load() {
			return nil
		}
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		defer func() { <-gate }()
		if completed.Load() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := release(ctx); err != nil {
			return err
		}
		completed.Store(true)
		return nil
	}
}
