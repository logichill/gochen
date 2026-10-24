package scoped

import (
	"context"

	"gochen/app/query"
	"gochen/auth/action"
	"gochen/errors"
)

// --- IReader ---

func (a *application[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	var zero T
	target, err := a.resolveBoundary(ctx, id)
	if err != nil {
		return zero, err
	}
	scopedCtx, err := a.authorizeRead(ctx, action.OpRead, target)
	if err != nil {
		return zero, err
	}
	// 恒用绑定数据范围的 scopedCtx 二次读取：返回值必须经数据范围过滤
	// 与 inner.Get 的后置处理。降级解析（仓储无边界探针）拿到的 target
	// 是用原始 ctx 预读的，直接返回会绕过范围过滤这道纵深防御
	// （PDP 资源级放行 ≠ 主体数据范围覆盖）。
	return a.inner.Get(scopedCtx, id)
}

func (a *application[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	// 与 Get 同理：Exists 也必须先解析目标资源边界再做资源级 PDP 判定。
	// 无目标退化到动作级判定会绕过 PDP 对目标实体属性的 ABAC 控制，
	// 形成存在性 Oracle 泄露（探测无权资源是否存在）。
	// 与 L1 装饰器一致用 read 动作码：Exists 问的是“这条记录在不在”，
	// 属于单条读语义。两层用不同动作码会让一次 Exists 同时要求 read 与 list 两个权限。
	target, err := a.resolveBoundary(ctx, id)
	if err != nil {
		if errors.Is(err, errors.NotFound) {
			return false, nil
		}
		return false, err
	}
	scopedCtx, err := a.authorizeRead(ctx, action.OpRead, target)
	if err != nil {
		// 目标级判定不通过时按“不存在”处理，避免向无权调用方泄露资源存在性。
		if errors.Is(err, errors.NotFound) {
			return false, nil
		}
		return false, err
	}
	return a.inner.Exists(scopedCtx, id)
}

func (a *application[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	scopedCtx, err := a.authorizeRead(ctx, action.OpList)
	if err != nil {
		return nil, err
	}
	return a.inner.List(scopedCtx, offset, limit)
}

func (a *application[T, ID]) Count(ctx context.Context) (int64, error) {
	scopedCtx, err := a.authorizeRead(ctx, action.OpList)
	if err != nil {
		return 0, err
	}
	return a.inner.Count(scopedCtx)
}

func (a *application[T, ID]) ListByQuery(ctx context.Context, quer *query.QueryRequest) ([]T, error) {
	scopedCtx, err := a.authorizeRead(ctx, action.OpList)
	if err != nil {
		return nil, err
	}
	return a.inner.ListByQuery(scopedCtx, quer)
}

func (a *application[T, ID]) ListPage(ctx context.Context, request *query.PaginationOptions) (*query.PagedResult[T], error) {
	scopedCtx, err := a.authorizeRead(ctx, action.OpList)
	if err != nil {
		return nil, err
	}
	return a.inner.ListPage(scopedCtx, request)
}

func (a *application[T, ID]) CountByQuery(ctx context.Context, quer *query.QueryRequest) (int64, error) {
	scopedCtx, err := a.authorizeRead(ctx, action.OpList)
	if err != nil {
		return 0, err
	}
	return a.inner.CountByQuery(scopedCtx, quer)
}
