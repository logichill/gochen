package auth

import "context"

// bindDerivedPrincipalDataScope 在未显式设置数据边界时，回填基于 principal 推导的 scope。
//
// 这样应用层即使只拿到了认证主体，也能获得一份与当前身份一致的默认数据边界。
func bindDerivedPrincipalDataScope(ctx context.Context, principal Principal) context.Context {
	if ctx == nil {
		return nil
	}
	if _, ok := DataScopeFromContext(ctx); ok && !dataScopeIsDerivedFromContext(ctx) {
		return ctx
	}
	scope, ok := principalDataScope(principal)
	if !ok {
		return withDerivedDataScope(ctx, DataScope{})
	}
	return withDerivedDataScope(ctx, scope)
}

// principalDataScope 把 principal 的活动 scope 投影为 DataScope。
func principalDataScope(principal Principal) (DataScope, bool) {
	principal = principal.Clone()
	switch {
	case principal.IsSystem && principal.ActiveScopeID == 0:
		return DataScope{Mode: ScopeModeGlobal}, true
	case principal.ActiveScopeID == 0:
		return DataScope{}, false
	default:
		return DataScope{ActiveScopeID: principal.ActiveScopeID, VisibleScopeIDs: []int64{principal.ActiveScopeID}, Mode: ScopeModeScoped}, true
	}
}
