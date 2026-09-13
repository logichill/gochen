package contextx

import (
	"context"

	"gochen/contextx/field"
)

// 标准化的上下文字段键名（用于消息/事件的跨进程传播）。
const (
	// MetadataTenantKey 定义租户字段键名。
	MetadataTenantKey = field.MetadataTenantKey
	// MetadataTraceKey 定义链路字段键名。
	MetadataTraceKey = field.MetadataTraceKey
	// MetadataRequestIDKey 定义请求字段键名。
	MetadataRequestIDKey = field.MetadataRequestIDKey
	// MetadataOperatorKey 定义操作人字段键名。
	MetadataOperatorKey = field.MetadataOperatorKey
)

// WithTraceID 返回携带 traceID 的 context。
func WithTraceID(ctx context.Context, traceID string) (context.Context, error) {
	return field.WithTraceID(ctx, traceID)
}

// TraceID 从 context 中获取 traceID。
func TraceID(ctx context.Context) string {
	return field.TraceID(ctx)
}

// WithRequestID 返回携带 requestID 的 context。
func WithRequestID(ctx context.Context, requestID string) (context.Context, error) {
	return field.WithRequestID(ctx, requestID)
}

// RequestID 从 context 中获取 requestID。
func RequestID(ctx context.Context) string {
	return field.RequestID(ctx)
}
