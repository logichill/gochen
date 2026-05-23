package decorators

import (
	"context"
	"gochen/contextx"
	"gochen/eventing"
)

// filterEventsByTenant 过滤事件列表按租户。
func filterEventsByTenant[ID comparable](ctx context.Context, events []eventing.Event[ID]) []eventing.Event[ID] {
	tenantID := contextx.TenantID(ctx)
	if tenantID == "" {
		return events
	}
	filtered := make([]eventing.Event[ID], 0, len(events))
	for i := range events {
		evt := events[i]
		v, ok := evt.GetMetadata().GetString(contextx.MetadataTenantKey)
		if ok && v == tenantID {
			filtered = append(filtered, evt)
		}
	}
	return filtered
}
