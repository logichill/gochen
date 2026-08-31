package writeflow

import "context"

// ForEach 返回对一组元素逐个执行 fn 的流程步骤。
func ForEach[T any](items []T, fn func(context.Context, T) error) Step {
	return func(ctx context.Context) error {
		for _, item := range items {
			if err := fn(ctx, item); err != nil {
				return err
			}
		}
		return nil
	}
}

// CallbacksFor 为一组元素构造 post-commit 回调，并过滤未配置的回调。
func CallbacksFor[T any](items []T, callback func(T) func(context.Context) error) []func(context.Context) error {
	if callback == nil {
		return nil
	}
	callbacks := make([]func(context.Context) error, 0, len(items))
	for _, item := range items {
		if cb := callback(item); cb != nil {
			callbacks = append(callbacks, cb)
		}
	}
	return callbacks
}
