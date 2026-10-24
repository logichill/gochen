package scoped

import (
	"context"

	"gochen/app/internal/depcheck"
	"gochen/auth/action"
	"gochen/errors"
)

// --- IWriter：PDP 判定 → 构造 IConstraintProvider → 经通道投放 ---

func (a *application[T, ID]) Create(ctx context.Context, e T) error {
	if depcheck.IsNil(e) {
		return errors.NewCode(errors.InvalidInput, "entity cannot be nil")
	}
	scopedCtx, err := a.authorizeWrite(ctx, action.OpCreate, e)
	if err != nil {
		return err
	}
	return a.inner.Create(scopedCtx, e)
}

func (a *application[T, ID]) Update(ctx context.Context, e T) error {
	if depcheck.IsNil(e) {
		return errors.NewCode(errors.InvalidInput, "entity cannot be nil")
	}
	// 授权输入必须是**仓储里的真实边界**，不能是客户端提交的实体。
	// 否则 PDP 看到的 owner / scope / revision 全由请求体决定：范围与租户虽然
	// 会被仓储的原子 WHERE 兜住，但 owner 这类不下推的属性一旦被策略引用，
	// 伪造归属即可让 PDP 放行一次本不该允许的写。
	target, err := a.resolveBoundary(ctx, e.GetID())
	if err != nil {
		return err
	}
	scopedCtx, err := a.authorizeWrite(ctx, action.OpUpdate, target)
	if err != nil {
		return err
	}
	return a.inner.Update(scopedCtx, e)
}

func (a *application[T, ID]) Delete(ctx context.Context, id ID) error {
	// 删除只有 ID，需先解析目标资源边界作为授权输入。
	target, err := a.resolveBoundary(ctx, id)
	if err != nil {
		return err
	}
	scopedCtx, err := a.authorizeWrite(ctx, action.OpDelete, target)
	if err != nil {
		return err
	}
	return a.inner.Delete(scopedCtx, id)
}
