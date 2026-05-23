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
