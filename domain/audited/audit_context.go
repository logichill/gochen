package audited

import (
	"context"
	"strings"
)

type auditResourceKindContextKey struct{}

// WithAuditResourceKind 绑定审计资源类型。
//
// 说明：当多个资源类型共享同一张审计表时，该值用于按 resource_kind 隔离审计记录。
func WithAuditResourceKind(ctx context.Context, kind string) context.Context {
	kind = strings.TrimSpace(kind)
	if ctx == nil || kind == "" {
		return ctx
	}
	return context.WithValue(ctx, auditResourceKindContextKey{}, kind)
}

// AuditResourceKind 返回 ctx 中绑定的审计资源类型。
func AuditResourceKind(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	kind, _ := ctx.Value(auditResourceKindContextKey{}).(string)
	return strings.TrimSpace(kind)
}
