// Package fields 提供轻量级 context 字段键和访问器。
package fields

import (
	stdctx "context"
	"strings"

	"gochen/errors"
)

// 标准化的上下文字段键名（用于消息/事件的跨进程传播）。
const (
	// MetadataTenantKey 定义租户字段键名。
	MetadataTenantKey = "tenant_id"
	// MetadataTraceKey 定义链路字段键名。
	MetadataTraceKey = "trace_id"
	// MetadataRequestIDKey 定义请求字段键名。
	MetadataRequestIDKey = "request_id"
	// MetadataOperatorKey 定义操作人字段键名。
	MetadataOperatorKey = "operator"
)

type principalKey uint8

type correlationKey uint8

const (
	keyTenantID principalKey = iota + 1
	keyOperator
)

const (
	keyTraceID correlationKey = iota + 1
	keyRequestID
)

func ensure(ctx stdctx.Context) (stdctx.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx, nil
}

// WithTenantID 返回携带 tenantID 的 context。
func WithTenantID(ctx stdctx.Context, tenantID string) (stdctx.Context, error) {
	ctx, err := ensure(ctx)
	if err != nil {
		return nil, err
	}
	return stdctx.WithValue(ctx, keyTenantID, tenantID), nil
}

// TenantID 从 context 中获取 tenantID。
func TenantID(ctx stdctx.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(keyTenantID).(string); ok && v != "" {
		return v
	}
	return ""
}

// WithOperator 返回携带 operator 的 context。
func WithOperator(ctx stdctx.Context, operator string) (stdctx.Context, error) {
	ctx, err := ensure(ctx)
	if err != nil {
		return nil, err
	}
	return stdctx.WithValue(ctx, keyOperator, strings.TrimSpace(operator)), nil
}

// Operator 从 context 中获取 operator。
func Operator(ctx stdctx.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(keyOperator).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// WithTraceID 返回携带 traceID 的 context。
func WithTraceID(ctx stdctx.Context, traceID string) (stdctx.Context, error) {
	ctx, err := ensure(ctx)
	if err != nil {
		return nil, err
	}
	return stdctx.WithValue(ctx, keyTraceID, traceID), nil
}

// TraceID 从 context 中获取 traceID。
func TraceID(ctx stdctx.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(keyTraceID).(string); ok && v != "" {
		return v
	}
	return ""
}

// WithRequestID 返回携带 requestID 的 context。
func WithRequestID(ctx stdctx.Context, requestID string) (stdctx.Context, error) {
	ctx, err := ensure(ctx)
	if err != nil {
		return nil, err
	}
	return stdctx.WithValue(ctx, keyRequestID, strings.TrimSpace(requestID)), nil
}

// RequestID 从 context 中获取 requestID。
func RequestID(ctx stdctx.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(keyRequestID).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
