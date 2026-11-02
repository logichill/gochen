package eventstore

import (
	"context"

	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/internal/depcheck"
	"gochen/observe/logging"
)

const (
	restoreAggregateBatchLimit     = 1000
	restoreAggregateBatchMaxRounds = 10000
)

// RestoreAggregate 根据快照与事件流恢复聚合。
//
// 说明：
// - 返回 RestoreResult 包含恢复详情。
// - 单次恢复最多读取 restoreAggregateBatchMaxRounds 个批次，用于防止异常存储实现导致无限分页。
func (a *domainEventStore[ID]) RestoreAggregate(ctx context.Context, aggregate deventsourced.IEventSourcedAggregate[ID]) (*deventsourced.RestoreResult, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depcheck.IsNil(aggregate) {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate cannot be nil")
	}

	result := &deventsourced.RestoreResult{}
	var fromVersion uint64

	// 若配置了 SnapshotManager，优先尝试通过快照 + 增量事件恢复：
	// 1) 通过 LoadSnapshot 将聚合状态恢复到快照版本；
	// 2) 记录快照版本，从该版本之后重放增量领域事件。
	if !depcheck.IsNil(a.snapshotManager) {
		snap, err := a.snapshotManager.LoadSnapshot(ctx, aggregate.GetID(), aggregate)
		if err == nil && snap != nil {
			fromVersion = snap.Version
			result.FromSnapshot = true
			result.SnapshotVersion = snap.Version

			// 显式设置聚合版本号（如果聚合支持 IVersionSettable 接口）
			if versioned, ok := aggregate.(deventsourced.IVersionSettable); ok {
				versioned.SetVersion(snap.Version)
			}

			a.logger.Debug(ctx, "aggregate restored from snapshot",
				logging.Any("aggregate_id", aggregate.GetID()),
				logging.Uint64("snapshot_version", snap.Version))
		} else if err != nil {
			a.logger.Info(ctx, "snapshot load failed, falling back to full replay",
				logging.Any("aggregate_id", aggregate.GetID()),
				logging.Error(err))
			// 降级为全量重放前必须把聚合重置到零值：LoadSnapshot 可能在失败前
			// 已把快照数据部分反序列化写入目标（自定义 RestoreFromSnapshotData）
			//，在其上重放事件会得到并集脏状态。重置失败则直接报错，
			// 宁可失败也不能返回错误聚合。
			if resetErr := resetAggregateForReplay(aggregate); resetErr != nil {
				return nil, errors.Wrap(resetErr, errors.Internal, "reset aggregate before full replay failed").
					WithContext("aggregate_id", aggregate.GetID())
			}
		}
	}

	applyOne := func(evt *eventing.Event[ID]) error {
		if evt == nil {
			return errors.NewCode(errors.InvalidInput, "event cannot be nil")
		}
		domainEvt, err := asDomainEvent(ctx, a.eventRegistry, a.upgraders, evt)
		if err != nil {
			return err
		}
		if err := aggregate.ApplyEvent(domainEvt); err != nil {
			var appErr *errors.AppError
			if errors.As(err, &appErr) && appErr != nil {
				return appErr.
					WithContext("event_type", evt.GetType()).
					WithContext("event_id", evt.GetID())
			}
			return errors.Wrap(err, errors.Internal, "apply event failed").
				WithContext("event_type", evt.GetType()).
				WithContext("event_id", evt.GetID())
		}
		return nil
	}

	// 使用流式接口分批读取，避免一次性加载大聚合事件流导致内存峰值/GC 压力。
	var lastVersion = fromVersion
	afterVersion := fromVersion
	for round := 1; ; round++ {
		if round > restoreAggregateBatchMaxRounds {
			return nil, errors.NewCode(errors.Internal, "restore aggregate exceeded max batch rounds").
				WithContext("aggregate_type", a.aggregateType).
				WithContext("aggregate_id", aggregate.GetID()).
				WithContext("after_version", afterVersion)
		}
		prevAfterVersion := afterVersion
		batch, err := a.eventStore.StreamAggregate(ctx, &store.AggregateStreamOptions[ID]{
			AggregateType: a.aggregateType,
			AggregateID:   aggregate.GetID(),
			AfterVersion:  afterVersion,
			Limit:         restoreAggregateBatchLimit,
		})
		if err != nil {
			if errors.Is(err, errors.NotFound) {
				// 部分实现以 NotFound 表示流末尾：此时前几轮可能已经重放事件
				// （或快照已设定版本），必须对齐提交状态，否则 expectedVersion
				// 停留在旧值，后续 Save 将引发非预期的乐观锁冲突。
				aggregate.MarkEventsAsCommitted()
				result.Version = lastVersion
				result.Exists = result.Version > 0
				return result, nil
			}
			return nil, err
		}
		if batch == nil || len(batch.Events) == 0 {
			break
		}
		batchLastVersion := batch.Events[len(batch.Events)-1].Version
		if batch.HasMore && batchLastVersion <= prevAfterVersion {
			return nil, errors.NewCode(errors.Internal, "restore aggregate stalled pagination").
				WithContext("aggregate_type", a.aggregateType).
				WithContext("aggregate_id", aggregate.GetID()).
				WithContext("after_version", prevAfterVersion).
				WithContext("next_version", batch.NextVersion).
				WithContext("last_version", batchLastVersion)
		}
		for i := range batch.Events {
			evt := &batch.Events[i]
			if err := applyOne(evt); err != nil {
				return nil, err
			}
			result.EventCount++
			lastVersion = evt.Version
		}
		if !batch.HasMore {
			break
		}
		// 使用 NextVersion 推进游标；若实现未提供 NextVersion，则退化为最后一条事件版本号。
		nextAfterVersion := batchLastVersion
		if batch.NextVersion > afterVersion {
			nextAfterVersion = batch.NextVersion
		}
		if nextAfterVersion <= prevAfterVersion {
			return nil, errors.NewCode(errors.Internal, "restore aggregate stalled pagination").
				WithContext("aggregate_type", a.aggregateType).
				WithContext("aggregate_id", aggregate.GetID()).
				WithContext("after_version", prevAfterVersion).
				WithContext("next_version", batch.NextVersion).
				WithContext("last_version", lastVersion)
		}
		afterVersion = nextAfterVersion
	}

	aggregate.MarkEventsAsCommitted()
	result.Version = lastVersion
	result.Exists = result.Version > 0
	return result, nil
}

// resetAggregateForReplay 在快照加载失败降级为全量重放前重置聚合簿记。
//
// 聚合实现 IReplayResettable 时调用其重置方法；自定义聚合未实现该接口时
// 无法保证目标未被部分污染，返回错误而非静默继续（fail-closed）。
func resetAggregateForReplay[ID comparable](aggregate deventsourced.IEventSourcedAggregate[ID]) error {
	if resettable, ok := any(aggregate).(deventsourced.IReplayResettable); ok && resettable != nil {
		resettable.ResetForReplay()
		return nil
	}
	return errors.NewCode(errors.Unsupported,
		"aggregate does not support replay reset; cannot safely fall back to full replay")
}

// Exists 检查聚合是否存在。
func (a *domainEventStore[ID]) Exists(ctx context.Context, aggregateID ID) (bool, error) {
	batch, err := a.eventStore.StreamAggregate(ctx, &store.AggregateStreamOptions[ID]{
		AggregateType: a.aggregateType,
		AggregateID:   aggregateID,
		Limit:         1,
	})
	if err != nil {
		if errors.Is(err, errors.NotFound) {
			return false, nil
		}
		return false, err
	}
	return batch != nil && len(batch.Events) > 0, nil
}

// GetAggregateVersion 从存储中查询对象。
//
// 说明：
// - GetAggregateVersion 获取当前聚合类型下指定聚合的当前版本。
func (a *domainEventStore[ID]) GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error) {
	version, err := a.eventStore.GetAggregateVersion(ctx, a.aggregateType, aggregateID)
	if err != nil {
		if errors.Is(err, errors.NotFound) {
			return 0, nil
		}
		return 0, err
	}
	return version, nil
}
