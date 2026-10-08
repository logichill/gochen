package eventstore

import (
	"context"

	"gochen/eventing"
	"gochen/eventing/store"
)

// IAggregateEventStore 是领域事件适配器所需的持久化能力。
// 聚合恢复使用分页读取；无需实现全局扫描、整段加载或单独的存在性查询。
type IAggregateEventStore[ID comparable] interface {
	store.IAggregateEventReader[ID]
	// AppendEvents 遵循 store.IEventStore 的版本检查、幂等与原子追加契约。
	AppendEvents(ctx context.Context, aggregateType string, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error
}
