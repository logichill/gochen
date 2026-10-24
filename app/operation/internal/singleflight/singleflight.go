// Package singleflight 提供进程内同 key 单 owner 执行原语。
package singleflight

import (
	"context"
	"fmt"
)

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
