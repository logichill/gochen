package eventsourced

import (
	"context"

	"gochen/errors"
)

// Get 从存储中查询对象。
//
// 说明：
// - Get 根据 ID 加载聚合（通过 RestoreAggregate 恢复）。
// - 若聚合不存在，返回 errors.NotFound。
// - factory 创建的聚合实例会在仓储侧显式绑定预编译元数据（若支持）。
func (r *EventSourcedRepository[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	if ctx == nil {
		var zero T
		return zero, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	aggregate, err := r.newAggregate(id)
	if err != nil {
		var zero T
		return zero, err
	}
	result, err := r.store.RestoreAggregate(ctx, aggregate)
	if err != nil {
		var zero T
		return zero, err
	}
	if !result.Exists {
		var zero T
		return zero, errors.NewCode(errors.NotFound, "aggregate not found").
			WithContext("aggregate_type", r.aggregateType).
			WithContext("id", id)
	}
	return aggregate, nil
}

// GetOrCreate 从存储中查询对象。
//
// 说明：
// - GetOrCreate 获取或创建聚合。
// - 若聚合存在则返回已有状态；若不存在则返回 factory 创建的新实例（version=0）。
// - 适用于"幂等创建"场景，调用方可通过 aggregate.GetVersion() == 0 判断是否为新建。
func (r *EventSourcedRepository[T, ID]) GetOrCreate(ctx context.Context, id ID) (T, error) {
	if ctx == nil {
		var zero T
		return zero, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	aggregate, err := r.newAggregate(id)
	if err != nil {
		var zero T
		return zero, err
	}
	_, err = r.store.RestoreAggregate(ctx, aggregate)
	if err != nil {
		var zero T
		return zero, err
	}
	return aggregate, nil
}

// Exists 检查聚合是否存在。
func (r *EventSourcedRepository[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	if ctx == nil {
		return false, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return r.store.Exists(ctx, id)
}

// GetAggregateVersion 从存储中查询对象。
//
// 说明：
// - GetAggregateVersion 获取聚合当前版本。
func (r *EventSourcedRepository[T, ID]) GetAggregateVersion(ctx context.Context, id ID) (uint64, error) {
	if ctx == nil {
		return 0, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	version, err := r.store.GetAggregateVersion(ctx, id)
	if err != nil {
		return 0, err
	}
	// 语义约定：不存在返回 0。
	return version, nil
}
