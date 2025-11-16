package httpx

import (
	"context"
	"fmt"

	"gochen/idgen/snowflake"
)

// Context keys for tracing
type contextKey string

const (
	contextKeyCorrelationID contextKey = "correlation_id"
	contextKeyCausationID   contextKey = "causation_id"
)

// WithCorrelationID 在 context 中设置 correlation_id
//
// Correlation ID 标识整个业务流程，从 HTTP 请求开始，
// 贯穿所有命令和事件。
//
// 示例:
//
//	ctx := httpx.WithCorrelationID(ctx, "req-123")
//	correlationID := httpx.GetCorrelationID(ctx) // "req-123"
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKeyCorrelationID, id)
}

// GetCorrelationID 从 context 中获取 correlation_id
//
// 如果不存在，返回空字符串。
func GetCorrelationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(contextKeyCorrelationID).(string); ok {
		return id
	}
	return ""
}

// WithCausationID 在 context 中设置 causation_id
//
// Causation ID 标识直接的因果关系，例如：
// - 触发命令的 HTTP 请求 ID
// - 触发事件的命令 ID
// - 触发下一个命令的事件 ID
//
// 示例:
//
//	ctx := httpx.WithCausationID(ctx, "cmd-456")
//	causationID := httpx.GetCausationID(ctx) // "cmd-456"
func WithCausationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKeyCausationID, id)
}

// GetCausationID 从 context 中获取 causation_id
//
// 如果不存在，返回空字符串。
func GetCausationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(contextKeyCausationID).(string); ok {
		return id
	}
	return ""
}

// GenerateCorrelationID 生成新的 correlation ID
//
// 使用 snowflake 算法生成唯一 ID。
func GenerateCorrelationID() string {
	return fmt.Sprintf("cor-%d", snowflake.Generate())
}

// GenerateCausationID 生成新的 causation ID
//
// 使用 snowflake 算法生成唯一 ID。
func GenerateCausationID() string {
	return fmt.Sprintf("cau-%d", snowflake.Generate())
}

// InjectTraceContext 将追踪上下文注入到 metadata
//
// 从 context 中提取 correlation_id 和 causation_id，
// 注入到提供的 metadata map 中。
//
// 示例:
//
//	metadata := make(map[string]interface{})
//	httpx.InjectTraceContext(ctx, metadata)
//	// metadata["correlation_id"] = "cor-123"
//	// metadata["causation_id"] = "cau-456"
func InjectTraceContext(ctx context.Context, metadata map[string]interface{}) {
	if ctx == nil || metadata == nil {
		return
	}

	if correlationID := GetCorrelationID(ctx); correlationID != "" {
		metadata["correlation_id"] = correlationID
	}

	if causationID := GetCausationID(ctx); causationID != "" {
		metadata["causation_id"] = causationID
	}
}

// ExtractTraceContext 从 metadata 提取追踪上下文
//
// 从 metadata map 中提取 correlation_id 和 causation_id，
// 注入到返回的 context 中。
//
// 示例:
//
//	metadata := map[string]interface{}{
//	    "correlation_id": "cor-123",
//	    "causation_id": "cau-456",
//	}
//	ctx := httpx.ExtractTraceContext(context.Background(), metadata)
func ExtractTraceContext(ctx context.Context, metadata map[string]interface{}) context.Context {
	if ctx == nil || metadata == nil {
		return ctx
	}

	if correlationID, ok := metadata["correlation_id"].(string); ok && correlationID != "" {
		ctx = WithCorrelationID(ctx, correlationID)
	}

	if causationID, ok := metadata["causation_id"].(string); ok && causationID != "" {
		ctx = WithCausationID(ctx, causationID)
	}

	return ctx
}
