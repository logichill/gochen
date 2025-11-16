package middleware

import (
	"context"

	"gochen/httpx"
	"gochen/messaging"
	"gochen/messaging/command"
	mmw "gochen/messaging/middleware"
)

// HttpTracingMiddleware 针对 HTTP 上下文的命令追踪中间件
//
// 它从 httpx.Context 中提取 correlation_id/causation_id 注入到命令 Metadata，
// 再委托通用的 TracingMiddleware 完成后续的命令/事件链路传播，从而与事件追踪模型保持一致。
type HttpTracingMiddleware struct {
	base *mmw.TracingMiddleware
}

// NewHttpTracingMiddleware 创建 HTTP 追踪中间件
func NewHttpTracingMiddleware() *HttpTracingMiddleware {
	return &HttpTracingMiddleware{
		base: mmw.NewTracingMiddleware(),
	}
}

// Name 返回中间件名称
func (m *HttpTracingMiddleware) Name() string {
	return "HttpTracingMiddleware"
}

// Handle 处理消息
func (m *HttpTracingMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	// 只对命令类型注入 HTTP 追踪上下文
	if _, ok := message.(*command.Command); ok {
		md := message.GetMetadata()
		httpx.InjectTraceContext(ctx, md)
	}

	// 交由通用 TracingMiddleware 统一处理命令/事件 trace 传播
	return m.base.Handle(ctx, message, next)
}
