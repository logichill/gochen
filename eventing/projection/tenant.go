package projection

import (
	"context"

	"gochen/eventing"
	httpx "gochen/http"
)

// TenantAwareProjector 租户感知的投影装饰器
//
// 自动过滤不属于当前租户的事件，确保投影只处理自己租户的数据。
//
// 过滤规则：
//  1. 提取 Context 中的租户 ID
//  2. 提取事件 Metadata 中的租户 ID
//  3. 只有租户 ID 匹配时才处理事件
//  4. 无租户上下文时，处理所有事件（向后兼容）
//
// 使用示例：
//
//	baseProjector := &MyProjector{}
//	tenantProjector := projection.NewTenantAwareProjector(baseProjector)
//
//	// 租户 A 的事件
//	ctx := httpx.WithTenantID(ctx, "tenant-A")
//	err := tenantProjector.Handle(ctx, eventA) // 处理
//
//	// 租户 B 的事件
//	err := tenantProjector.Handle(ctx, eventB) // 跳过
type TenantAwareProjector struct {
	projector IProjection
}

// NewTenantAwareProjector 创建租户感知的投影
func NewTenantAwareProjector(p IProjection) IProjection {
	return &TenantAwareProjector{
		projector: p,
	}
}

// GetName 返回投影名称
func (p *TenantAwareProjector) GetName() string {
	return p.projector.GetName()
}

// Handle 处理事件（自动过滤租户）
func (p *TenantAwareProjector) Handle(ctx context.Context, event eventing.IEvent) error {
	// 提取当前租户 ID
	contextTenantID := httpx.GetTenantID(ctx)
	if contextTenantID == "" {
		// 无租户上下文，处理所有事件（向后兼容）
		return p.projector.Handle(ctx, event)
	}

	eventTenantID := extractTenantIDFromMessage(event)

	// 只处理属于当前租户的事件
	if eventTenantID == contextTenantID {
		return p.projector.Handle(ctx, event)
	}

	// 跳过其他租户的事件
	return nil
}

// GetSupportedEventTypes 获取支持的事件类型
func (p *TenantAwareProjector) GetSupportedEventTypes() []string {
	return p.projector.GetSupportedEventTypes()
}

func (p *TenantAwareProjector) GetStatus() ProjectionStatus {
	return p.projector.GetStatus()
}

// Rebuild 重建投影
func (p *TenantAwareProjector) Rebuild(ctx context.Context, events []eventing.Event) error {
	// 过滤租户事件
	contextTenantID := httpx.GetTenantID(ctx)
	if contextTenantID == "" {
		// 无租户上下文，处理所有事件
		return p.projector.Rebuild(ctx, events)
	}

	// 过滤事件
	filteredEvents := make([]eventing.Event, 0, len(events))
	for _, event := range events {
		eventTenantID := extractTenantIDFromEvent(&event)
		if eventTenantID == contextTenantID {
			filteredEvents = append(filteredEvents, event)
		}
	}

	return p.projector.Rebuild(ctx, filteredEvents)
}

// extractTenantIDFromEvent 从事件中提取租户 ID
func extractTenantIDFromEvent(event *eventing.Event) string {
	if event == nil {
		return ""
	}
	if tenantID, ok := event.Metadata["tenant_id"].(string); ok {
		return tenantID
	}
	return ""
}

func extractTenantIDFromMessage(event eventing.IEvent) string {
	if event == nil {
		return ""
	}
	if msg := event.GetMetadata(); msg != nil {
		if tenantID, ok := msg["tenant_id"].(string); ok {
			return tenantID
		}
	}
	return ""
}
