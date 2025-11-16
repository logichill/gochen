package middleware

import (
	"context"

	"gochen/httpx"
	"gochen/messaging"
	"gochen/messaging/command"
)

// tracingMiddleware 追踪中间件实现
type tracingMiddleware struct{}

// TracingMiddleware 追踪中间件
//
// 自动将 Context 中的 correlation_id 和 causation_id 注入到命令的 Metadata 中。
//
// 追踪 ID 流转规则：
//  1. Correlation ID - 保持不变，标识整个业务流程
//  2. Causation ID - 设置为当前命令的 ID，标识直接因果关系
//
// 使用示例：
//
//	// 注意：由于当前 MessageBus 的中间件机制限制，
//	// 建议在命令处理器中手动注入追踪 ID
//	ctx := httpx.WithCorrelationID(ctx, "cor-123")
//	ctx = httpx.WithCausationID(ctx, "req-456")
//	httpx.InjectTraceContext(ctx, cmd.Metadata)
func TracingMiddleware() *tracingMiddleware {
	return &tracingMiddleware{}
}

// Name 返回中间件名称
func (m *tracingMiddleware) Name() string {
	return "TracingMiddleware"
}

// Handle 处理消息
func (m *tracingMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	// 只处理命令类型
	if cmd, ok := message.(*command.Command); ok {
		// 从 Context 提取追踪 ID
		correlationID := httpx.GetCorrelationID(ctx)
		causationID := httpx.GetCausationID(ctx)

		// 注入到命令的 Metadata
		if correlationID != "" {
			cmd.WithMetadata("correlation_id", correlationID)
		}
		if causationID != "" {
			cmd.WithMetadata("causation_id", causationID)
		} else {
			// 若上下文没有 causation_id，则将其设为当前命令 ID（形成清晰的因果链）
			if cmd.GetID() != "" {
				cmd.WithMetadata("causation_id", cmd.GetID())
			}
		}
	}

	// 执行下一个处理器
	return next(ctx, message)
}
