package contextx

import (
	stdctx "context"

	"gochen/contextx/fields"
)

// 标准化的上下文字段键名（用于消息/事件的跨进程传播）。
const (
	// MetadataTenantKey 定义租户字段键名。
	MetadataTenantKey = fields.MetadataTenantKey
	// MetadataTraceKey 定义链路字段键名。
	MetadataTraceKey = fields.MetadataTraceKey
	// MetadataRequestIDKey 定义请求字段键名。
	MetadataRequestIDKey = fields.MetadataRequestIDKey
	// MetadataOperatorKey 定义操作人字段键名。
	MetadataOperatorKey = fields.MetadataOperatorKey
)

// WithTraceID 返回携带 traceID 的 context。
func WithTraceID(ctx stdctx.Context, traceID string) (stdctx.Context, error) {
	return fields.WithTraceID(ctx, traceID)
}

// TraceID 从 context 中获取 traceID。
func TraceID(ctx stdctx.Context) string {
	return fields.TraceID(ctx)
}

// WithRequestID 返回携带 requestID 的 context。
func WithRequestID(ctx stdctx.Context, requestID string) (stdctx.Context, error) {
	return fields.WithRequestID(ctx, requestID)
}

// RequestID 从 context 中获取 requestID。
func RequestID(ctx stdctx.Context) string {
	return fields.RequestID(ctx)
}
