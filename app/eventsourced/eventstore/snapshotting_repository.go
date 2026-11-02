package eventstore

import (
	"context"

	"gochen/app/eventsourced/internal/aggregatecheck"
	"gochen/contextx"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/internal/depcheck"
	"gochen/observe/logging"
)

// SnapshottingRepositoryOptions 快照装饰器配置。
type SnapshottingRepositoryOptions[ID comparable] struct {
	// SnapshotManager 快照管理器；为空时禁用快照功能（直接返回 inner）。
	SnapshotManager ISnapshotWriter[ID]

	// FailOnError 为 true 时：快照策略或写入失败返回 contextx.AfterCommitError，主保存已成功；
	// 为 false 时：快照失败仅记录日志，不影响主写路径（推荐默认）。
	FailOnError bool

	Logger logging.ILogger
}

// snapshottingRepository 在单聚合主保存成功后按策略写入快照。
// 本装饰器仅提供 IEventSourcedRepository 能力，不透传原子批量 SaveAll。
type snapshottingRepository[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	inner         deventsourced.IEventSourcedRepository[T, ID]
	snapshotMgr   ISnapshotWriter[ID]
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
	if depcheck.IsNil(inner) {
		return nil, errors.NewCode(errors.InvalidInput, "inner repository cannot be nil")
	}
	if depcheck.IsNil(opts.SnapshotManager) {
		return inner, nil
	}
	if _, atomic := inner.(deventsourced.IAtomicEventSourcedRepository[T, ID]); atomic {
		return nil, errors.NewCode(errors.Unsupported, "snapshot decorator does not support atomic repository; snapshot committed aggregates explicitly")
	}

	logger := opts.Logger
	if depcheck.IsNil(logger) {
		logger = logging.ComponentLogger("app.eventsourced.snapshotting_repository").
			WithField("aggregate_type", aggregateType)
	}

	return &snapshottingRepository[T, ID]{
		inner:         inner,
		snapshotMgr:   opts.SnapshotManager,
		failOnError:   opts.FailOnError,
		logger:        logger,
		aggregateType: aggregateType,
	}, nil
}

// Save 保存数据。
func (r *snapshottingRepository[T, ID]) Save(ctx context.Context, aggregate T) error {
	if err := aggregatecheck.ValidateInstance(aggregate, r.aggregateType); err != nil {
		return err
	}
	if err := r.inner.Save(ctx, aggregate); err != nil {
		return err
	}
	if r.snapshotMgr == nil {
		return nil
	}

	should, err := r.snapshotMgr.ShouldCreateSnapshot(ctx, aggregate)
	if err != nil {
		if r.failOnError {
			return contextx.WrapAfterCommitError(err)
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
			return contextx.WrapAfterCommitError(err)
		}
		r.logger.Warn(ctx, "create snapshot failed",
			logging.Any("aggregate_id", aggregate.GetID()),
			logging.Uint64("version", aggregate.GetVersion()),
			logging.Error(err))
	}

	return nil
}

// Get 从存储中查询对象。
func (r *snapshottingRepository[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	return r.inner.Get(ctx, id)
}

// GetOrCreate 从存储中查询对象。
func (r *snapshottingRepository[T, ID]) GetOrCreate(ctx context.Context, id ID) (T, error) {
	return r.inner.GetOrCreate(ctx, id)
}

// Exists 判断对象是否存在。
func (r *snapshottingRepository[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	return r.inner.Exists(ctx, id)
}

// GetAggregateVersion 从存储中查询对象。
func (r *snapshottingRepository[T, ID]) GetAggregateVersion(ctx context.Context, id ID) (uint64, error) {
	return r.inner.GetAggregateVersion(ctx, id)
}
