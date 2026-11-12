package eventstore

import (
	"context"
	"gochen/domain"
	"gochen/errors"
	"gochen/eventing/store"
	"gochen/internal/depcheck"
)

// NewEventStream 为显式状态模型创建完整事件流适配器，复用事件信封、升级、追加与发布模式。
// 不读取或写入旧聚合快照；配置 SnapshotManager 时拒绝装配，避免静默改变快照格式。
func NewEventStream[ID comparable](opts DomainEventStoreOptions[ID]) (*domainEventStore[ID], error) {
	if !depcheck.IsNil(opts.SnapshotManager) {
		return nil, errors.NewCode(errors.InvalidInput, "explicit state replay does not use aggregate snapshots")
	}
	return newDomainEventStore(opts)
}

// ReadEvents 从版本 1 顺序读取并解码完整事件流；严格拒绝缺失、重复或倒序版本。
// 回调失败或取消时版本结果无效，调用方必须丢弃部分状态。不存在返回零版本。
func (a *domainEventStore[ID]) ReadEvents(ctx context.Context, id ID, apply func(domain.IDomainEvent) error) (uint64, error) {
	if ctx == nil || apply == nil {
		return 0, errors.NewCode(errors.InvalidInput, "context and event callback are required")
	}
	var version uint64
	for round := 0; round < restoreAggregateBatchMaxRounds; round++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		batch, err := a.eventStore.StreamAggregate(ctx, &store.AggregateStreamOptions[ID]{AggregateType: a.aggregateType, AggregateID: id, AfterVersion: version, Limit: restoreAggregateBatchLimit})
		if err != nil {
			if errors.Is(err, errors.NotFound) {
				return version, ctx.Err()
			}
			return 0, err
		}
		if batch == nil || len(batch.Events) == 0 {
			if batch != nil && batch.HasMore {
				return 0, errors.NewCode(errors.Internal, "event stream returned an empty unfinished batch")
			}
			return version, ctx.Err()
		}
		for i := range batch.Events {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			evt := &batch.Events[i]
			if evt.Version != version+1 {
				return 0, errors.NewCode(errors.Internal, "event stream versions must be contiguous")
			}
			event, err := asDomainEvent(ctx, a.eventRegistry, a.upgraders, evt)
			if err != nil {
				return 0, err
			}
			if err := apply(event); err != nil {
				return 0, err
			}
			version = evt.Version
		}
		if !batch.HasMore {
			return version, ctx.Err()
		}
	}
	return 0, errors.NewCode(errors.Internal, "event stream exceeded max batch rounds")
}
