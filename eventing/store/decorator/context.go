package decorator

import (
	"context"
	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
)

// ContextAwareEventStore 是贯通 tenant/trace/operator 的事件存储装饰器。
//
// 行为：
//   - AppendEvents：
//   - tenant/operator：ctx 有值时以 ctx 为准，覆盖每个事件 Metadata 中缺失或不一致的 tenant_id/operator；
//   - trace：确保批内每个事件的 metadata.trace_id 一致并补齐（ctx 优先；ctx 缺失时继承批内唯一 trace_id；批内不一致则返回 INVALID_INPUT）。
//   - LoadEvents/Stream*：若 ctx 中存在 tenant_id，则仅返回 metadata.tenant_id 与之相等的事件；否则不做过滤。
type ContextAwareEventStore[ID comparable] struct {
	inner store.IEventStreamStore[ID]
}

// Inner 返回被装饰的事件存储实现。
//
// 说明：
// - 用于与其他装饰器组合（composition），例如：TenantAwareEventStore 可基于该装饰器实现。
// - 不建议在业务代码中用它“绕过装饰器”，否则可能破坏 tenant/trace/operator 等约束的一致性。
func (s *ContextAwareEventStore[ID]) Inner() store.IEventStreamStore[ID] {
	if s == nil {
		return nil
	}
	return s.inner
}

// NewContextAwareEventStore 创建一个上下文感知的事件存储装饰器，用于自动注入 tenant/trace/operator 信息。
func NewContextAwareEventStore[ID comparable](inner store.IEventStreamStore[ID]) *ContextAwareEventStore[ID] {
	return &ContextAwareEventStore[ID]{inner: inner}
}

func (s *ContextAwareEventStore[ID]) AppendEvents(ctx context.Context, aggregateType string, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error {
	if s == nil || s.inner == nil {
		return errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	var err error
	ctx, _, err = ensureBatchTraceID(ctx, events)
	if err != nil {
		return err
	}
	for _, evt := range events {
		if evt == nil {
			continue
		}
		if err := contextx.InjectTenantID(ctx, evt.GetMetadata()); err != nil {
			return err
		}
		if err := contextx.InjectOperator(ctx, evt.GetMetadata()); err != nil {
			return err
		}
	}
	return s.inner.AppendEvents(ctx, aggregateType, aggregateID, events, expectedVersion)
}

// LoadEvents 加载事件并按 tenant 过滤（ctx 携带 tenant_id 时生效）。
func (s *ContextAwareEventStore[ID]) LoadEvents(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	if s == nil || s.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	events, err := s.inner.LoadEvents(ctx, aggregateType, aggregateID, afterVersion)
	if err != nil {
		return nil, err
	}
	return filterEventsByTenant(ctx, events), nil
}

// StreamEvents 流式加载事件并按 tenant 过滤（ctx 携带 tenant_id 时生效）。
func (s *ContextAwareEventStore[ID]) StreamEvents(ctx context.Context, opts *store.StreamOptions) (*store.StreamResult[ID], error) {
	if s == nil || s.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	tenantID := contextx.TenantID(ctx)
	if tenantID == "" {
		return s.inner.StreamEvents(ctx, opts)
	}

	cur := &store.StreamOptions{}
	if opts != nil {
		*cur = *opts
	}
	for {
		res, err := s.inner.StreamEvents(ctx, cur)
		if err != nil {
			return nil, err
		}
		if res == nil {
			return nil, nil
		}
		filtered := filterEventsByTenant(ctx, res.Events)
		if len(filtered) > 0 || !res.HasMore || res.NextCursor == "" {
			return &store.StreamResult[ID]{
				Events:       filtered,
				NextCursor:   res.NextCursor,
				EventCursors: filterEventCursors(res.Events, res.EventCursors, filtered),
				HasMore:      res.HasMore,
			}, nil
		}
		if cur.After == res.NextCursor {
			return &store.StreamResult[ID]{
				Events:       filtered,
				NextCursor:   res.NextCursor,
				EventCursors: filterEventCursors(res.Events, res.EventCursors, filtered),
				HasMore:      res.HasMore,
			}, nil
		}
		cur.After = res.NextCursor
	}
}

func filterEventCursors[ID comparable](all []eventing.Event[ID], cursors []string, filtered []eventing.Event[ID]) []string {
	if len(filtered) == 0 || len(cursors) == 0 {
		return nil
	}
	byID := make(map[string]string, len(all))
	for i := range all {
		if i < len(cursors) && cursors[i] != "" {
			byID[all[i].GetID()] = cursors[i]
		}
	}
	result := make([]string, 0, len(filtered))
	for i := range filtered {
		if cursor := byID[filtered[i].GetID()]; cursor != "" {
			result = append(result, cursor)
		}
	}
	if len(result) != len(filtered) {
		return nil
	}
	return result
}

// StreamAggregate 流式加载单个聚合的事件并按 tenant 过滤（ctx 携带 tenant_id 时生效）。
func (s *ContextAwareEventStore[ID]) StreamAggregate(ctx context.Context, opts *store.AggregateStreamOptions[ID]) (*store.AggregateStreamResult[ID], error) {
	if s == nil || s.inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	tenantID := contextx.TenantID(ctx)
	if tenantID == "" {
		return s.inner.StreamAggregate(ctx, opts)
	}

	cur := &store.AggregateStreamOptions[ID]{}
	if opts != nil {
		*cur = *opts
	}

	afterVersion := cur.AfterVersion
	for {
		cur.AfterVersion = afterVersion
		res, err := s.inner.StreamAggregate(ctx, cur)
		if err != nil {
			return nil, err
		}
		if res == nil {
			return nil, nil
		}
		filtered := filterEventsByTenant(ctx, res.Events)
		nextAfter := res.NextVersion
		if len(filtered) > 0 || !res.HasMore || nextAfter <= afterVersion {
			return &store.AggregateStreamResult[ID]{Events: filtered, HasMore: res.HasMore, NextVersion: nextAfter}, nil
		}
		afterVersion = nextAfter
	}
}

// HasAggregate 检查聚合是否存在，并保持与租户过滤一致。
func (s *ContextAwareEventStore[ID]) HasAggregate(ctx context.Context, aggregateType string, aggregateID ID) (bool, error) {
	events, err := s.LoadEvents(ctx, aggregateType, aggregateID, 0)
	if err != nil {
		return false, err
	}
	return len(events) > 0, nil
}

func (s *ContextAwareEventStore[ID]) GetAggregateVersion(ctx context.Context, aggregateType string, aggregateID ID) (uint64, error) {
	if s == nil || s.inner == nil {
		return 0, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	if contextx.TenantID(ctx) == "" {
		return s.inner.GetAggregateVersion(ctx, aggregateType, aggregateID)
	}

	var version uint64
	afterVersion := uint64(0)
	for {
		res, err := s.StreamAggregate(ctx, &store.AggregateStreamOptions[ID]{
			AggregateType: aggregateType,
			AggregateID:   aggregateID,
			AfterVersion:  afterVersion,
			Limit:         store.DefaultStreamLimit,
		})
		if err != nil {
			return 0, err
		}
		if res == nil || len(res.Events) == 0 {
			return version, nil
		}
		version = res.Events[len(res.Events)-1].GetVersion()
		if !res.HasMore || res.NextVersion <= afterVersion {
			return version, nil
		}
		afterVersion = res.NextVersion
	}
}

var _ store.IEventStreamStore[int64] = (*ContextAwareEventStore[int64])(nil)
