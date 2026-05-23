package decorators

import (
	"context"
	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
)

// TenantAwareEventStore 是租户感知的事件存储装饰器。
//
// 行为：
//   - AppendEvents：将 ctx 中的 tenant_id 注入到每个事件的 Metadata（若未设置）。
//   - LoadEvents：若 ctx 中存在 tenant_id，则仅返回 metadata.tenant_id 与之相等的事件；
//     若 ctx 中没有 tenant_id，则不做过滤。
type TenantAwareEventStore[ID comparable] struct {
	inner store.IEventStreamStore[ID]
	base  *ContextAwareEventStore[ID]
}

// NewTenantAwareEventStore 创建租户Aware事件存储。
func NewTenantAwareEventStore[ID comparable](inner store.IEventStreamStore[ID]) *TenantAwareEventStore[ID] {
	// TenantAware 是 ContextAware 的子集：只做 tenant_id 注入与过滤（不注入 trace/operator）。
	//
	// 说明：
	// - 为减少重复实现，这里复用 ContextAwareEventStore 的“租户过滤 + 跳过空页”逻辑（读路径）；
	// - 写路径（AppendEvents）保持 TenantAware 的原有语义：仅注入 tenant_id。
	return &TenantAwareEventStore[ID]{
		inner: inner,
		base:  NewContextAwareEventStore(inner),
	}
}

func (s *TenantAwareEventStore[ID]) contextDecorator() *ContextAwareEventStore[ID] {
	// 优先使用构造时预置的装饰器；若 base 被手动构造为“空 inner”，则回退为基于 s.inner 的装饰器。
	// （这里不做写入，避免引入潜在数据竞争；正常路径应通过 NewTenantAwareEventStore 构造。）
	if s.base != nil && s.base.Inner() != nil {
		return s.base
	}
	return NewContextAwareEventStore(s.inner)
}

// AppendEvents 向事件存储追加事件。
func (s *TenantAwareEventStore[ID]) AppendEvents(ctx context.Context, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error {
	if s == nil || s.inner == nil {
		return errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	for _, evt := range events {
		if evt == nil {
			continue
		}
		if err := contextx.InjectTenantID(ctx, evt.GetMetadata()); err != nil {
			return err
		}
	}
	return s.inner.AppendEvents(ctx, aggregateID, events, expectedVersion)
}

// LoadEvents 加载聚合事件。
func (s *TenantAwareEventStore[ID]) LoadEvents(ctx context.Context, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	if s == nil || s.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.contextDecorator().LoadEvents(ctx, aggregateID, afterVersion)
}

// LoadEventsByType 加载指定聚合类型的事件。
func (s *TenantAwareEventStore[ID]) LoadEventsByType(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	if s == nil || s.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.contextDecorator().LoadEventsByType(ctx, aggregateType, aggregateID, afterVersion)
}

// StreamEvents 按游标遍历事件流。
func (s *TenantAwareEventStore[ID]) StreamEvents(ctx context.Context, opts *store.StreamOptions) (*store.StreamResult[ID], error) {
	if s == nil || s.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.contextDecorator().StreamEvents(ctx, opts)
}

// StreamAggregate 遍历指定聚合的事件流。
func (s *TenantAwareEventStore[ID]) StreamAggregate(ctx context.Context, opts *store.AggregateStreamOptions[ID]) (*store.AggregateStreamResult[ID], error) {
	if s == nil || s.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.contextDecorator().StreamAggregate(ctx, opts)
}

// HasAggregate 检查聚合是否存在。
func (s *TenantAwareEventStore[ID]) HasAggregate(ctx context.Context, aggregateID ID) (bool, error) {
	if s == nil || s.inner == nil {
		return false, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.contextDecorator().HasAggregate(ctx, aggregateID)
}

func (s *TenantAwareEventStore[ID]) GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error) {
	if s == nil || s.inner == nil {
		return 0, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return s.contextDecorator().GetAggregateVersion(ctx, aggregateID)
}

var _ store.IEventStreamStore[int64] = (*TenantAwareEventStore[int64])(nil)
