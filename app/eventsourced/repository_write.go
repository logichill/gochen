package eventsourced

import (
	"context"

	"gochen/app/eventsourced/internal/aggregatecheck"
	"gochen/contextx"
	"gochen/errors"
)

// Save 持久化聚合上的未提交事件。
//
// 说明：
// - 仓储使用聚合显式暴露的 `GetExpectedVersion()` 作为乐观锁基线版本，
// - 不再通过“当前版本号 - 未提交事件数量”做反推。
func (r *EventSourcedRepository[T, ID]) Save(ctx context.Context, aggregate T) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := aggregatecheck.ValidateInstance(aggregate, r.aggregateType); err != nil {
		return err
	}
	events := aggregate.GetUncommittedEvents()
	if len(events) == 0 {
		return nil
	}

	if err := r.store.AppendEvents(ctx, aggregate.GetID(), events, aggregate.GetExpectedVersion()); err != nil {
		if contextx.IsAfterCommitError(err) {
			aggregate.MarkEventsAsCommitted()
		}
		return err
	}

	aggregate.MarkEventsAsCommitted()
	return nil
}
