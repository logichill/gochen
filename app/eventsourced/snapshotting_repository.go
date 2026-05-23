package eventsourced

import (
	"context"

	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing/store/snapshot"
	"gochen/logging"
)

// SnapshottingRepositoryOptions 快照装饰器配置。
type SnapshottingRepositoryOptions[ID comparable] struct {
	// SnapshotManager 快照管理器；为空时禁用快照功能（直接返回 inner）。
	SnapshotManager *snapshot.Manager[ID]

	// FailOnError 为 true 时：创建快照失败将导致 Save 返回错误；
	// 为 false 时：快照失败仅记录日志，不影响主写路径（推荐默认）。
	FailOnError bool

	Logger logging.ILogger
}

// SnapshottingRepository 定义Snapshotting仓储实现。
type SnapshottingRepository[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	inner         deventsourced.IEventSourcedRepository[T, ID]
	snapshotMgr   *snapshot.Manager[ID]
	failOnError   bool
	logger        logging.ILogger
	aggregateType string
}

// NewSnapshottingRepository 创建Snapshotting仓储。
func NewSnapshottingRepository[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	aggregateType string,
	inner deventsourced.IEventSourcedRepository[T, ID],
	opts SnapshottingRepositoryOptions[ID],
) (deventsourced.IEventSourcedRepository[T, ID], error) {
	if inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner repository cannot be nil")
	}
	if opts.SnapshotManager == nil {
		return inner, nil
	}

	logger := opts.Logger
	if logger == nil {
		logger = logging.ComponentLogger("app.eventsourced.snapshotting_repository").
			WithField("aggregate_type", aggregateType)
	}

	return &SnapshottingRepository[T, ID]{
		inner:         inner,
		snapshotMgr:   opts.SnapshotManager,
		failOnError:   opts.FailOnError,
		logger:        logger,
		aggregateType: aggregateType,
	}, nil
}

// Save 保存数据。
func (r *SnapshottingRepository[T, ID]) Save(ctx context.Context, aggregate T) error {
	if err := r.inner.Save(ctx, aggregate); err != nil {
		return err
	}
	if r.snapshotMgr == nil {
		return nil
	}
	if any(aggregate) == nil {
		return errors.NewCode(errors.InvalidInput, "aggregate cannot be nil")
	}

	should, err := r.snapshotMgr.ShouldCreateSnapshot(ctx, aggregate)
	if err != nil {
		if r.failOnError {
			return err
		}
		r.logger.Warn(ctx, "snapshot policy check failed",
			logging.Any("aggregate_id", aggregate.GetID()),
			logging.Error(err))
		return nil
	}
	if !should {
		return nil
	}

	if err := r.snapshotMgr.CreateSnapshot(ctx, aggregate.GetID(), aggregate.GetAggregateType(), aggregate, aggregate.GetVersion()); err != nil {
		if r.failOnError {
			return err
		}
		r.logger.Warn(ctx, "create snapshot failed",
			logging.Any("aggregate_id", aggregate.GetID()),
			logging.Uint64("version", aggregate.GetVersion()),
			logging.Error(err))
	}

	return nil
}

// Get 从存储中查询对象。
func (r *SnapshottingRepository[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	return r.inner.Get(ctx, id)
}

// GetOrCreate 从存储中查询对象。
func (r *SnapshottingRepository[T, ID]) GetOrCreate(ctx context.Context, id ID) (T, error) {
	return r.inner.GetOrCreate(ctx, id)
}

// Exists 判断对象是否存在。
func (r *SnapshottingRepository[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	return r.inner.Exists(ctx, id)
}

// GetAggregateVersion 从存储中查询对象。
func (r *SnapshottingRepository[T, ID]) GetAggregateVersion(ctx context.Context, id ID) (uint64, error) {
	return r.inner.GetAggregateVersion(ctx, id)
}
