package app

import (
	"context"

	"gochen/app/query"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
)

// Query 在声明的范围和 ID 意图内读取完整实体，并冻结普通写保护基线。
// 命令读取完整资源事实；字段投影由业务输出阶段完成。
func (r *CommandRepository[T, ID]) Query(ctx context.Context, options query.QueryOptions) ([]T, error) {
	if err := r.grant.check(ctx); err != nil {
		return nil, err
	}
	if len(options.Fields) > 0 {
		return nil, errors.NewCode(errors.InvalidInput, "command queries require complete resource facts")
	}
	reader, ok := r.repo.(query.IQueryableRepository[T])
	if !ok {
		return nil, errors.NewCode(errors.Unsupported, "command repository does not support queries")
	}
	frozen, err := meta.Clone(options)
	if err != nil {
		return nil, err
	}
	entities, err := reader.Query(ctx, frozen, r.readCondition())
	if err != nil {
		return nil, err
	}
	result := make([]T, 0, len(entities))
	for _, entity := range entities {
		tracked, err := r.track(entity)
		if err != nil {
			return nil, err
		}
		result = append(result, tracked)
	}
	return result, nil
}

// QueryCount 使用与实体查询相同的授权范围和请求 ID 条件统计资源。
func (r *CommandRepository[T, ID]) QueryCount(ctx context.Context, options query.QueryOptions) (int64, error) {
	if err := r.grant.check(ctx); err != nil {
		return 0, err
	}
	reader, ok := r.repo.(query.IQueryableRepository[T])
	if !ok {
		return 0, errors.NewCode(errors.Unsupported, "command repository does not support query counts")
	}
	frozen, err := meta.Clone(options)
	if err != nil {
		return 0, err
	}
	return reader.QueryCount(ctx, frozen, r.readCondition())
}

func (r *CommandRepository[T, ID]) readCondition() spec.Spec {
	return r.grant.readCondition()
}

func (g *CommandGrant) readCondition() spec.Spec {
	condition := g.decision.Spec
	if len(g.request.IDs) > 0 {
		values := make([]any, 0, len(g.request.IDs))
		for _, id := range g.request.IDs {
			values = append(values, id)
		}
		condition = spec.And(condition, spec.In(meta.ID(), values...))
	}
	return condition
}
