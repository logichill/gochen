package auth

import (
	"context"
	"strings"

	"gochen/contextx"
	"gochen/errors"
)

type principalContextKey struct{}

// Principal 表达当前操作主体的最小授权语义。
type Principal struct {
	SubjectID       int64
	HomeTenantID    int64
	HomeScopeID     int64
	ActiveScopeID   int64
	ActiveBindingID int64
	Roles           []string
	Permissions     []string
	PermissionSet   map[string]struct{}
	IsSystem        bool
}

func (p Principal) Clone() Principal {
	cloned := Principal{
		SubjectID:       NormalizePositiveID(p.SubjectID),
		HomeTenantID:    NormalizePositiveID(p.HomeTenantID),
		HomeScopeID:     NormalizePositiveID(p.HomeScopeID),
		ActiveScopeID:   NormalizePositiveID(p.ActiveScopeID),
		ActiveBindingID: NormalizePositiveID(p.ActiveBindingID),
		Roles:           normalizeStrings(p.Roles),
		Permissions:     normalizeStrings(p.Permissions),
		IsSystem:        p.IsSystem,
	}
	if len(p.PermissionSet) > 0 {
		cloned.PermissionSet = cloneSet(p.PermissionSet)
	}
	if cloned.PermissionSet == nil && len(cloned.Permissions) > 0 {
		cloned.PermissionSet = toSet(cloned.Permissions)
	}
	return cloned
}

// HasPermission 判断主体是否显式持有某个权限。
func (p Principal) HasPermission(permission string) bool {
	permission = strings.TrimSpace(permission)
	if permission == "" {
		return false
	}
	principal := p.Clone()
	_, ok := principal.PermissionSet[permission]
	return ok
}

// AllowsPermission 判断主体是否满足某个权限要求。
//
// 约定：
// - IsSystem 主体直接放行；
// - 允许显式授予全通配权限 `*:*:*`（常用于平台管理员）；
// - 其他通配 pattern（如 `api:user:*`）按段匹配。
func (p Principal) AllowsPermission(permission string) bool {
	permission = strings.TrimSpace(permission)
	if permission == "" {
		return true
	}
	principal := p.Clone()
	if principal.IsSystem {
		return true
	}
	if principal.HasPermission(permission) {
		return true
	}
	for _, granted := range principal.Permissions {
		if PermissionPatternMatches(granted, permission) {
			return true
		}
	}
	return false
}

// WithPrincipal 将主体信息写入 context。
func WithPrincipal(ctx context.Context, principal Principal) (context.Context, error) {
	ctx, err := contextx.Ensure(ctx)
	if err != nil {
		return nil, err
	}
	principal = principal.Clone()
	ctx, err = WithUserID(ctx, principal.SubjectID)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, principalContextKey{}, principal)
	if eval, ok := ctx.Value(authzEvalContextKey{}).(AuthzEvalContext); ok {
		eval = normalizeEvalContext(eval)
		eval.Principal = principal
		ctx = context.WithValue(ctx, authzEvalContextKey{}, normalizeEvalContext(eval))
	}
	ctx = bindDerivedPrincipalDataScope(ctx, principal)
	return ctx, nil
}

// PrincipalFromContext 从 context 中读取主体。
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	if principal, ok := ctx.Value(principalContextKey{}).(Principal); ok {
		return principal.Clone(), true
	}
	principal := Principal{SubjectID: UserID(ctx)}
	if principal.SubjectID == 0 {
		return Principal{}, false
	}
	return principal.Clone(), true
}

// RequirePrincipal 从 context 中读取主体；缺失时返回 Unauthorized。
func RequirePrincipal(ctx context.Context) (Principal, error) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return Principal{}, errors.NewCode(errors.Unauthorized, "principal is required")
	}
	return principal, nil
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func cloneSet(values map[string]struct{}) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]struct{}, len(values))
	for value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		cloned[value] = struct{}{}
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}
