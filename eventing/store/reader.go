package store

import (
	"context"

	"gochen/eventing"
)

// IEventReader 读取聚合事件历史与当前版本，不授予追加能力。
type IEventReader[ID comparable] interface {
	// LoadEvents 按版本升序读取 afterVersion 之后的事件，不包含该版本。
	LoadEvents(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error)
	// GetAggregateVersion 返回当前最大版本；聚合不存在时返回 0。
	GetAggregateVersion(ctx context.Context, aggregateType string, aggregateID ID) (uint64, error)
}

// IAggregateEventReader 为恢复与历史分页提供聚合版本和分批读取能力。
type IAggregateEventReader[ID comparable] interface {
	// GetAggregateVersion 返回当前最大版本；聚合不存在时返回 0。
	GetAggregateVersion(ctx context.Context, aggregateType string, aggregateID ID) (uint64, error)
	// StreamAggregate 按版本升序分页；NextVersion 是下一次请求的 AfterVersion。
	StreamAggregate(ctx context.Context, opts *AggregateStreamOptions[ID]) (*AggregateStreamResult[ID], error)
}

// IEventStreamReader 提供全局事件扫描，用于投影回放，不要求聚合读写能力。
type IEventStreamReader[ID comparable] interface {
	// StreamEvents 按后端定义的全局顺序分页，游标应原样保存并用于后续请求。
	StreamEvents(ctx context.Context, opts *StreamOptions) (*StreamResult[ID], error)
}
