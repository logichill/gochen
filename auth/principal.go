package auth

import (
	"context"
	"strings"

	"gochen/contextx"
	"gochen/errors"
)

// Principal 只放身份：主体、租户、当前范围与系统标记。
type Principal struct {
	SubjectID string
	TenantID  string
	ScopeID   int64
	System    bool
}

// SystemInfo 记录系统身份执行的发起人与原因。
type SystemInfo struct {
	Initiator string
	Reason    string
}

type principalKey struct{}
type principalBindingKey struct{}
type systemKey struct{}

// WithPrincipal 把主体写入 ctx，并清除前一次身份的系统执行记录。
func WithPrincipal(ctx context.Context, principal Principal) (context.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "context cannot be nil")
	}
	principal.SubjectID = strings.TrimSpace(principal.SubjectID)
	principal.TenantID = strings.TrimSpace(principal.TenantID)
	if principal.SubjectID == "" && !principal.System {
		return nil, errors.NewCode(errors.Unauthorized, "principal subject is required")
	}
	current, ok := PrincipalFromContext(ctx)
	if !ok || ctx.Value(principalBindingKey{}) == nil || current.SubjectID != principal.SubjectID ||
		current.TenantID != principal.TenantID || current.ScopeID != principal.ScopeID || current.System != principal.System {
		// 标记属于当前派生链；身份回切也会建立新绑定，原 context 保持不变。
		ctx = context.WithValue(ctx, principalBindingKey{}, new(uint8))
	}
	ctx = context.WithValue(ctx, principalKey{}, principal)
	// 用非 SystemInfo 值遮蔽父 context 的旧执行记录。
	return context.WithValue(ctx, systemKey{}, struct{}{}), nil
}

// PrincipalFromContext 读取 ctx 中的主体。
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

// HasSamePrincipalBinding 判断两个上下文是否沿用同一次主体与当前租户绑定。
// WithPrincipal 改变主体、租户、范围或系统身份时产生新绑定；回切保持旧绑定失效。
// AsSystem 保留现有根绑定。
// 缺少主体绑定或上下文为空时返回 false。
func HasSamePrincipalBinding(ctx, bound context.Context) bool {
	if ctx == nil || bound == nil {
		return false
	}
	binding := ctx.Value(principalBindingKey{})
	return binding != nil && binding == bound.Value(principalBindingKey{}) &&
		contextx.HasSameTenantBinding(ctx, bound)
}

// RequirePrincipal 读取 ctx 中的主体，缺失时返回 Unauthorized。
func RequirePrincipal(ctx context.Context) (Principal, error) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return Principal{}, errors.NewCode(errors.Unauthorized, "principal is required")
	}
	return principal, nil
}

// AsSystem 派生系统身份的 ctx，并记录发起人与原因。
func AsSystem(ctx context.Context, reason string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	info := SystemInfo{Reason: strings.TrimSpace(reason)}
	principal, ok := PrincipalFromContext(ctx)
	if ok {
		info.Initiator = principal.SubjectID
	}
	principal.System = true
	ctx = context.WithValue(ctx, principalKey{}, principal)
	return context.WithValue(ctx, systemKey{}, info)
}

// SystemInfoFromContext 读取系统身份信息。
func SystemInfoFromContext(ctx context.Context) (SystemInfo, bool) {
	if ctx == nil {
		return SystemInfo{}, false
	}
	info, ok := ctx.Value(systemKey{}).(SystemInfo)
	return info, ok
}
