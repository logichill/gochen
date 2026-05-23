package auth

import (
	stdctx "context"
	"gochen/contextx"
)

const (
	// MetadataTenantKey 表示 tenant 元数据键。
	MetadataTenantKey = contextx.MetadataTenantKey
	// MetadataOperatorKey 表示 operator 元数据键。
	MetadataOperatorKey = contextx.MetadataOperatorKey
)

// WithTenantID 将 tenant 语义写入上下文。
func WithTenantID(ctx stdctx.Context, tenantID string) (stdctx.Context, error) {
	return contextx.WithTenantID(ctx, tenantID)
}

// TenantID 从上下文读取 tenant。
func TenantID(ctx stdctx.Context) string { return contextx.TenantID(ctx) }

// WithOperator 将操作人写入上下文。
func WithOperator(ctx stdctx.Context, operator string) (stdctx.Context, error) {
	return contextx.WithOperator(ctx, operator)
}

// Operator 从上下文读取操作人。
func Operator(ctx stdctx.Context) string { return contextx.Operator(ctx) }

// WithUserID 将主体用户 ID 写入上下文。
func WithUserID(ctx stdctx.Context, userID int64) (stdctx.Context, error) {
	return contextx.WithUserID(ctx, userID)
}

// UserID 从上下文读取主体用户 ID。
func UserID(ctx stdctx.Context) int64 { return contextx.UserID(ctx) }

// WithSessionID 将会话 ID 写入上下文。
func WithSessionID(ctx stdctx.Context, sessionID string) (stdctx.Context, error) {
	return contextx.WithSessionID(ctx, sessionID)
}

// SessionID 从上下文读取会话 ID。
func SessionID(ctx stdctx.Context) string { return contextx.SessionID(ctx) }

// WithSessionVisibility 设置当前链路是否允许读取会话语义。
func WithSessionVisibility(ctx stdctx.Context, allowed bool) (stdctx.Context, error) {
	return contextx.WithSessionVisibility(ctx, allowed)
}

func SessionVisible(ctx stdctx.Context) bool { return contextx.SessionVisible(ctx) }
