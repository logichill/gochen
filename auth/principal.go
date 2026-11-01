package auth

import (
	"context"
	"strings"

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
type systemKey struct{}

// WithPrincipal 把主体写入 ctx。
func WithPrincipal(ctx context.Context, principal Principal) (context.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "context cannot be nil")
	}
	principal.SubjectID = strings.TrimSpace(principal.SubjectID)
	principal.TenantID = strings.TrimSpace(principal.TenantID)
	if principal.SubjectID == "" && !principal.System {
		return nil, errors.NewCode(errors.Unauthorized, "principal subject is required")
	}
	return context.WithValue(ctx, principalKey{}, principal), nil
}

// PrincipalFromContext 读取 ctx 中的主体。
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
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
