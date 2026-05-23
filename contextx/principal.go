// Package contextx 提供协议无关的运行时上下文语义。
//
// 设计目标：
// - 统一承载运行时上下文中的主体语义（tenant/operator/user/session）；
// - 统一承载链路语义（trace/request）与跨进程传播 helper；
// - 保持与 HTTP / messaging / eventing 等传输层解耦，便于在 worker/CLI/HTTP/消息消费中复用。
package contextx

import (
	stdctx "context"
	"strings"

	"gochen/contextx/fields"
)

type principalKey uint8

const (
	keyUserID principalKey = iota + 1
	keySessionID
	keySessionVisible
)

// WithTenantID 返回携带 tenantID 的 context。
func WithTenantID(ctx stdctx.Context, tenantID string) (stdctx.Context, error) {
	return fields.WithTenantID(ctx, tenantID)
}

// TenantID 从 context 中获取 tenantID。
func TenantID(ctx stdctx.Context) string {
	return fields.TenantID(ctx)
}

// WithOperator 返回携带 operator 的 context。
func WithOperator(ctx stdctx.Context, operator string) (stdctx.Context, error) {
	return fields.WithOperator(ctx, operator)
}

// Operator 从 context 中获取 operator。
func Operator(ctx stdctx.Context) string {
	return fields.Operator(ctx)
}

// WithUserID 返回携带 userID 的 context。
func WithUserID(ctx stdctx.Context, userID int64) (stdctx.Context, error) {
	ctx, err := Ensure(ctx)
	if err != nil {
		return nil, err
	}
	return stdctx.WithValue(ctx, keyUserID, userID), nil
}

// UserID 从 context 中获取 userID。
func UserID(ctx stdctx.Context) int64 {
	if ctx == nil {
		return 0
	}
	if v, ok := ctx.Value(keyUserID).(int64); ok {
		return v
	}
	return 0
}

// WithSessionID 返回携带 sessionID 的 context。
func WithSessionID(ctx stdctx.Context, sessionID string) (stdctx.Context, error) {
	ctx, err := Ensure(ctx)
	if err != nil {
		return nil, err
	}
	return stdctx.WithValue(ctx, keySessionID, strings.TrimSpace(sessionID)), nil
}

// SessionID 从 context 中获取 sessionID；当当前链路禁止 session 可见时返回空字符串。
func SessionID(ctx stdctx.Context) string {
	if ctx == nil || !SessionVisible(ctx) {
		return ""
	}
	if v, ok := ctx.Value(keySessionID).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// WithSessionVisibility 返回携带 session 可见性策略的 context。
func WithSessionVisibility(ctx stdctx.Context, allowed bool) (stdctx.Context, error) {
	ctx, err := Ensure(ctx)
	if err != nil {
		return nil, err
	}
	return stdctx.WithValue(ctx, keySessionVisible, allowed), nil
}

// SessionVisible 返回当前链路是否允许读取 session 语义；默认允许。
func SessionVisible(ctx stdctx.Context) bool {
	if ctx == nil {
		return true
	}
	if v, ok := ctx.Value(keySessionVisible).(bool); ok {
		return v
	}
	return true
}
