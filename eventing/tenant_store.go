package eventing

import (
	"context"
	"time"

	httpx "gochen/http"
)

// TenantAwareEventStore 租户感知的事件存储装饰器
//
// 自动将 Context 中的租户 ID 注入到事件的 Metadata 中，
// 并在加载事件时自动过滤租户数据。
//
// 租户隔离规则：
//  1. 保存事件时：自动注入 tenant_id 到 Metadata
//  2. 加载事件时：自动过滤，只返回属于当前租户的事件
//  3. 无租户上下文时：不做任何过滤（向后兼容）
//
// 使用示例：
//
//	baseStore := store.NewMemoryEventStore()
//	tenantStore := eventing.NewTenantAwareEventStore(baseStore)
//
//	// 保存事件时，自动注入租户 ID
//	ctx := httpx.WithTenantID(ctx, "tenant-123")
//	err := tenantStore.AppendEvents(ctx, aggregateID, events, 0)
//	// 所有事件的 Metadata["tenant_id"] = "tenant-123"
//
//	// 加载事件时，自动过滤
//	ctx := httpx.WithTenantID(ctx, "tenant-123")
//	events, err := tenantStore.LoadEvents(ctx, aggregateID, 0)
//	// 只返回 tenant_id = "tenant-123" 的事件
type TenantAwareEventStore struct {
	store interface {
		AppendEvents(ctx context.Context, aggregateID int64, events []IStorableEvent, expectedVersion uint64) error
		LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]Event, error)
		StreamEvents(ctx context.Context, fromTime time.Time) ([]Event, error)
	}
}

// NewTenantAwareEventStore 创建租户感知的事件存储
func NewTenantAwareEventStore(store interface {
	AppendEvents(ctx context.Context, aggregateID int64, events []IStorableEvent, expectedVersion uint64) error
	LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]Event, error)
	StreamEvents(ctx context.Context, fromTime time.Time) ([]Event, error)
}) *TenantAwareEventStore {
	return &TenantAwareEventStore{
		store: store,
	}
}

// AppendEvents 追加事件（自动注入租户 ID）
func (s *TenantAwareEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []IStorableEvent, expectedVersion uint64) error {
	// 自动注入租户 ID 到所有事件
	tenantID := httpx.GetTenantID(ctx)
	if tenantID != "" {
		for _, event := range events {
			s.injectTenantID(tenantID, event)
		}
	}

	return s.store.AppendEvents(ctx, aggregateID, events, expectedVersion)
}

// LoadEvents 加载事件（自动过滤租户）
func (s *TenantAwareEventStore) LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]Event, error) {
	events, err := s.store.LoadEvents(ctx, aggregateID, afterVersion)
	if err != nil {
		return nil, err
	}

	// 过滤租户
	return s.filterEventsByTenant(ctx, events), nil
}

// StreamEvents 流式读取事件（自动过滤租户）
func (s *TenantAwareEventStore) StreamEvents(ctx context.Context, fromTime time.Time) ([]Event, error) {
	events, err := s.store.StreamEvents(ctx, fromTime)
	if err != nil {
		return nil, err
	}

	// 过滤租户
	return s.filterEventsByTenant(ctx, events), nil
}

// injectTenantID 注入租户 ID 到事件
func (s *TenantAwareEventStore) injectTenantID(tenantID string, event IStorableEvent) {
	if e, ok := event.(*Event); ok {
		if e.Metadata == nil {
			e.Metadata = make(map[string]any)
		}
		e.Metadata["tenant_id"] = tenantID
	}
}

// filterEventsByTenant 按租户过滤事件
func (s *TenantAwareEventStore) filterEventsByTenant(ctx context.Context, events []Event) []Event {
	// 提取当前租户 ID
	tenantID := httpx.GetTenantID(ctx)
	if tenantID == "" {
		// 无租户上下文，返回全部事件（向后兼容）
		return events
	}

	// 过滤事件
	filtered := make([]Event, 0, len(events))
	for _, event := range events {
		eventTenantID, _ := event.Metadata["tenant_id"].(string)
		if eventTenantID == tenantID {
			filtered = append(filtered, event)
		}
	}

	return filtered
}
