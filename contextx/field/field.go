// Package field 提供轻量级 context 字段键和访问器。
package field

import (
	"context"
	"strings"

	"gochen/errors"
)

// 标准化的上下文字段键名（用于消息/事件的跨进程传播）。
const (
	MetadataTenantKey    = "tenant_id"
	MetadataTraceKey     = "trace_id"
	MetadataRequestIDKey = "request_id"
	MetadataOperatorKey  = "operator"
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

func ensure(ctx context.Context) (context.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx, nil
}

func WithTenantID(ctx context.Context, tenantID string) (context.Context, error) {
	ctx, err := ensure(ctx)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, keyTenantID, strings.TrimSpace(tenantID)), nil
}

func TenantID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(keyTenantID).(string)
	return strings.TrimSpace(value)
}

func WithOperator(ctx context.Context, operator string) (context.Context, error) {
	ctx, err := ensure(ctx)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, keyOperator, strings.TrimSpace(operator)), nil
}

func Operator(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(keyOperator).(string)
	return strings.TrimSpace(value)
}

func WithTraceID(ctx context.Context, traceID string) (context.Context, error) {
	ctx, err := ensure(ctx)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, keyTraceID, strings.TrimSpace(traceID)), nil
}

func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(keyTraceID).(string)
	return strings.TrimSpace(value)
}

func WithRequestID(ctx context.Context, requestID string) (context.Context, error) {
	ctx, err := ensure(ctx)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, keyRequestID, strings.TrimSpace(requestID)), nil
}

func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(keyRequestID).(string)
	return strings.TrimSpace(value)
}
