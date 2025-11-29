package eventing

import (
	"context"
	"time"

	httpx "gochen/http"
)

// TracingEventStore 带追踪的事件存储装饰器
//
// 自动将 Context 中的 correlation_id 和 causation_id 注入到事件的 Metadata 中。
//
// 追踪 ID 流转规则：
//  1. Correlation ID - 保持不变，标识整个业务流程
//  2. Causation ID - 通常是触发事件的命令 ID
//
// 使用示例：
//
//	baseStore := store.NewMemoryEventStore()
//	tracingStore := eventing.NewTracingEventStore(baseStore)
//
//	// 追加事件时，自动注入追踪 ID
//	ctx := httpx.WithCorrelationID(ctx, "cor-123")
//	ctx = httpx.WithCausationID(ctx, "cmd-456")
//	err := tracingStore.AppendEvents(ctx, aggregateID, events, 0)
//	// 所有事件的 Metadata["correlation_id"] = "cor-123"
//	// 所有事件的 Metadata["causation_id"] = "cmd-456"
type TracingEventStore struct {
	store interface {
		AppendEvents(ctx context.Context, aggregateID int64, events []IStorableEvent, expectedVersion uint64) error
		LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]Event, error)
		StreamEvents(ctx context.Context, fromTime time.Time) ([]Event, error)
	}
}

// NewTracingEventStore 创建带追踪的事件存储
func NewTracingEventStore(store interface {
	AppendEvents(ctx context.Context, aggregateID int64, events []IStorableEvent, expectedVersion uint64) error
	LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]Event, error)
	StreamEvents(ctx context.Context, fromTime time.Time) ([]Event, error)
}) *TracingEventStore {
	return &TracingEventStore{
		store: store,
	}
}

// AppendEvents 追加事件（自动注入追踪 ID）
func (s *TracingEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []IStorableEvent, expectedVersion uint64) error {
	// 自动注入追踪 ID 到所有事件
	for _, event := range events {
		s.injectTraceContext(ctx, event)
	}

	return s.store.AppendEvents(ctx, aggregateID, events, expectedVersion)
}

// LoadEvents 加载事件
func (s *TracingEventStore) LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]Event, error) {
	return s.store.LoadEvents(ctx, aggregateID, afterVersion)
}

// StreamEvents 流式读取事件
func (s *TracingEventStore) StreamEvents(ctx context.Context, fromTime time.Time) ([]Event, error) {
	return s.store.StreamEvents(ctx, fromTime)
}

// injectTraceContext 注入追踪上下文到事件
func (s *TracingEventStore) injectTraceContext(ctx context.Context, event IStorableEvent) {
	// 从 Context 提取追踪 ID
	correlationID := httpx.GetCorrelationID(ctx)
	causationID := httpx.GetCausationID(ctx)

	// 注入到事件的 Metadata
	if e, ok := event.(*Event); ok {
		if e.Metadata == nil {
			e.Metadata = make(map[string]any)
		}
		if correlationID != "" {
			e.Metadata["correlation_id"] = correlationID
		}
		if causationID != "" {
			e.Metadata["causation_id"] = causationID
		}
	}
}
