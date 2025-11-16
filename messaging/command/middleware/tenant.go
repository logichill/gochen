package middleware

import (
	"context"

	"gochen/httpx"
	"gochen/messaging"
	"gochen/messaging/command"
)

// tenantMiddleware 租户中间件实现
type tenantMiddleware struct{}

// TenantMiddleware 租户中间件
//
// 自动将 Context 中的租户 ID 注入到命令的 Metadata 中。
//
// 租户 ID 流转规则：
//  1. 从 Context 提取租户 ID
//  2. 注入到命令的 Metadata["tenant_id"]
//
// 使用示例：
//
//	// 注意：由于当前 MessageBus 的中间件机制限制，
//	// 建议在命令处理器中手动注入租户 ID
//	ctx := httpx.WithTenantID(ctx, "tenant-123")
//	httpx.InjectTenantID(ctx, cmd.Metadata)
func TenantMiddleware() *tenantMiddleware {
	return &tenantMiddleware{}
}

// Name 返回中间件名称
func (m *tenantMiddleware) Name() string {
	return "TenantMiddleware"
}

// Handle 处理消息
func (m *tenantMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	// 只处理命令类型
	if cmd, ok := message.(*command.Command); ok {
		// 从 Context 提取租户 ID
		tenantID := httpx.GetTenantID(ctx)

		// 注入到命令的 Metadata
		if tenantID != "" {
			cmd.WithMetadata("tenant_id", tenantID)
		}
	}

	// 执行下一个处理器
	return next(ctx, message)
}
