package writeflow

import (
	"context"

	"gochen/errors"
)

// Each 将单元素步骤提升为切片载荷步骤；任一失败立即返回。fn 为 nil 时返回 nil。
func Each[T any](fn func(context.Context, T) error) func(context.Context, []T) error {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, items []T) error {
		for _, item := range items {
			if err := fn(ctx, item); err != nil {
				return err
			}
		}
		return nil
	}
}

// EachPure 将单元素校验提升为切片载荷校验；任一失败立即返回。fn 为 nil 时返回 nil。
func EachPure[T any](fn func(T) error) func([]T) error {
	if fn == nil {
		return nil
	}
	return func(items []T) error {
		for _, item := range items {
			if err := fn(item); err != nil {
				return err
			}
		}
		return nil
	}
}

// EachAll 将单元素步骤提升为切片载荷步骤，全部执行后合并错误（post-commit 语义）。
// fn 为 nil 时返回 nil。
func EachAll[T any](fn func(context.Context, T) error) func(context.Context, []T) error {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, items []T) error {
		var errs []error
		for _, item := range items {
			if err := fn(ctx, item); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}

// Join 将多个同签名步骤组合为一个（跳过 nil），任一失败立即返回。
func Join[P any](fns ...func(context.Context, P) error) func(context.Context, P) error {
	var kept []func(context.Context, P) error
	for _, fn := range fns {
		if fn != nil {
			kept = append(kept, fn)
		}
	}
	switch len(kept) {
	case 0:
		return nil
	case 1:
		return kept[0]
	}
	return func(ctx context.Context, payload P) error {
		for _, fn := range kept {
			if err := fn(ctx, payload); err != nil {
				return err
			}
		}
		return nil
	}
}

// DedupIDs 保持首次出现顺序去重，使批量操作对重复 ID 幂等。
func DedupIDs[ID comparable](ids []ID) []ID {
	if len(ids) <= 1 {
		return ids
	}
	seen := make(map[ID]struct{}, len(ids))
	out := make([]ID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
