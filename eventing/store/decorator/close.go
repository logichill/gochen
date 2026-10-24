package decorator

import (
	"context"
	"time"

	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
)

// closeInnerStore 将 Close 调用转发到底层存储。
//
// 装饰器自身不持有协议级资源（连接池、文件句柄等都在底层实现里），
// 但组合链 CachedEventStore(Tenant(context(SQL))) 依赖每一层把 Close
// 透传下去，否则底层存储的 Close 永远不会被调用，连接池无法随应用
// 生命周期优雅关闭。
//
// 通过 any(...) 断言可选的 io.Closer 形态：底层实现不提供 Close 时不报错。
func closeInnerStore(inner any) error {
	if inner == nil {
		return nil
	}
	if closer, ok := inner.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// Close 转发关闭到 ContextAwareEventStore 的底层存储。
func (s *ContextAwareEventStore[ID]) Close() error {
	if s == nil {
		return nil
	}
	return closeInnerStore(s.inner)
}

// Close 转发关闭到 TenantAwareEventStore 的底层存储。
func (s *TenantAwareEventStore[ID]) Close() error {
	if s == nil {
		return nil
	}
	return closeInnerStore(s.contextDecorator())
}

// Close 转发关闭到 TracingEventStore 的底层存储。
func (s *TracingEventStore[ID]) Close() error {
	if s == nil {
		return nil
	}
	return closeInnerStore(s.inner)
}

// Close 转发关闭到 MetricsEventStore 的底层存储。
func (s *MetricsEventStore[ID]) Close() error {
	if s == nil {
		return nil
	}
	return closeInnerStore(s.inner)
}

// AppendEventsBatch 转发原子批量追加（并记录批量耗时与事件数指标）。
func (m *MetricsEventStore[ID]) AppendEventsBatch(ctx context.Context, batch []store.EventAppend[ID]) error {
	if m == nil || isNil(m.inner) {
		return errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	atomic, ok := any(m.inner).(store.IAtomicEventStore[ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "inner event store does not support atomic batch append")
	}
	start := time.Now()
	err := atomic.AppendEventsBatch(ctx, batch)
	if !isNil(m.mr) {
		count := 0
		for i := range batch {
			count += len(batch[i].Events)
		}
		m.mr.RecordAppend(count, time.Since(start), err != nil)
	}
	return err
}

// AppendEventsBatch 转发原子批量追加，并保持每条流的租户注入与整体 trace 一致语义。
func (s *ContextAwareEventStore[ID]) AppendEventsBatch(ctx context.Context, batch []store.EventAppend[ID]) error {
	if s == nil || s.inner == nil {
		return errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	atomicInner, ok := any(s.inner).(store.IAtomicEventStore[ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "inner event store does not support atomic batch append")
	}
	// 与 AppendEvents 同口径：整批 trace 一致 + 逐事件注入 tenant/operator。
	all := make([]eventing.IStorableEvent[ID], 0)
	for i := range batch {
		all = append(all, batch[i].Events...)
	}
	var err error
	ctx, _, err = ensureBatchTraceID(ctx, all)
	if err != nil {
		return err
	}
	for i := range batch {
		for _, evt := range batch[i].Events {
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
	}
	return atomicInner.AppendEventsBatch(ctx, batch)
}

// AppendEventsBatch 转发原子批量追加，并保持租户注入语义（要求 ctx 携带租户）。
func (s *TenantAwareEventStore[ID]) AppendEventsBatch(ctx context.Context, batch []store.EventAppend[ID]) error {
	if s == nil || s.inner == nil {
		return errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	if err := requireTenantContext(ctx); err != nil {
		return err
	}
	for i := range batch {
		for index, evt := range batch[i].Events {
			if isNilStorableEvent(evt) {
				return errors.NewCode(errors.InvalidInput, "event cannot be nil").
					WithContext("stream_index", i).
					WithContext("event_index", index)
			}
			metadata := evt.GetMetadata()
			if metadata == nil {
				return errors.NewCode(errors.InvalidInput, "event metadata cannot be nil").
					WithContext("stream_index", i).
					WithContext("event_index", index)
			}
			if err := contextx.InjectTenantID(ctx, metadata); err != nil {
				return err
			}
		}
	}
	atomicInner, ok := any(s.inner).(store.IAtomicEventStore[ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "inner event store does not support atomic batch append")
	}
	return atomicInner.AppendEventsBatch(ctx, batch)
}

// AppendEventsBatch 转发原子批量追加，并保持整批 trace 一致语义。
func (s *TracingEventStore[ID]) AppendEventsBatch(ctx context.Context, batch []store.EventAppend[ID]) error {
	if s == nil || s.inner == nil {
		return errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	all := make([]eventing.IStorableEvent[ID], 0)
	for i := range batch {
		all = append(all, batch[i].Events...)
	}
	var err error
	ctx, _, err = ensureBatchTraceID(ctx, all)
	if err != nil {
		return err
	}
	atomicInner, ok := any(s.inner).(store.IAtomicEventStore[ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "inner event store does not support atomic batch append")
	}
	return atomicInner.AppendEventsBatch(ctx, batch)
}
