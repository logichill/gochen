package decorators

import (
	"context"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
)

// TracingEventStore 是带追踪信息注入的事件存储装饰器。
//
// 行为：
// - AppendEvents：确保批内每个事件的 metadata.trace_id 一致并补齐（ctx 优先；ctx 缺失时继承批内唯一 trace_id；批内不一致则返回 INVALID_INPUT）。
// - LoadEvents 透传到底层 store，不做修改。
type TracingEventStore[ID comparable] struct {
	inner store.IEventStreamStore[ID]
}

// NewTracingEventStore 创建Tracing事件存储。
func NewTracingEventStore[ID comparable](inner store.IEventStreamStore[ID]) *TracingEventStore[ID] {
	return &TracingEventStore[ID]{inner: inner}
}

// AppendEvents 向事件存储追加事件。
func (s *TracingEventStore[ID]) AppendEvents(ctx context.Context, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error {
	if s == nil || s.inner == nil {
		return errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	var err error
	ctx, _, err = ensureBatchTraceID(ctx, events)
	if err != nil {
		return err
	}
	return s.inner.AppendEvents(ctx, aggregateID, events, expectedVersion)
}

// LoadEvents 加载聚合事件。
func (s *TracingEventStore[ID]) LoadEvents(ctx context.Context, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	if s == nil || s.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.inner.LoadEvents(ctx, aggregateID, afterVersion)
}

// LoadEventsByType 加载指定聚合类型的事件。
func (s *TracingEventStore[ID]) LoadEventsByType(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	if s == nil || s.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.inner.LoadEventsByType(ctx, aggregateType, aggregateID, afterVersion)
}

// StreamEvents 按游标遍历事件流。
func (s *TracingEventStore[ID]) StreamEvents(ctx context.Context, opts *store.StreamOptions) (*store.StreamResult[ID], error) {
	if s == nil || s.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.inner.StreamEvents(ctx, opts)
}

// StreamAggregate 遍历指定聚合的事件流。
func (s *TracingEventStore[ID]) StreamAggregate(ctx context.Context, opts *store.AggregateStreamOptions[ID]) (*store.AggregateStreamResult[ID], error) {
	if s == nil || s.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.inner.StreamAggregate(ctx, opts)
}

// HasAggregate 检查聚合是否存在。
func (s *TracingEventStore[ID]) HasAggregate(ctx context.Context, aggregateID ID) (bool, error) {
	if s == nil || s.inner == nil {
		return false, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.inner.HasAggregate(ctx, aggregateID)
}

func (s *TracingEventStore[ID]) GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error) {
	if s == nil || s.inner == nil {
		return 0, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.inner.GetAggregateVersion(ctx, aggregateID)
}

var _ store.IEventStreamStore[int64] = (*TracingEventStore[int64])(nil)
