package auth

import (
	"context"
	"sort"

	"gochen/errors"
)

// ScopeMode 表示 DataScope 的约束方式。
type ScopeMode string

const (
	// ScopeModeGlobal 表示当前主体不受数据边界限制。
	ScopeModeGlobal ScopeMode = "global"
	// ScopeModeScoped 表示按可见 scope 集合进行约束。
	ScopeModeScoped ScopeMode = "scoped"
)

// DataScope 表达默认可见/可操作的数据边界。
type DataScope struct {
	ActiveScopeID   int64
	VisibleScopeIDs []int64
	TenantIDs       []int64
	Mode            ScopeMode
}

type dataScopeBinding struct {
	Scope     DataScope
	IsDerived bool
}

// IDataScopeResolver 定义 DataScope 求值器接口。
type IDataScopeResolver interface {
	Resolve(ctx context.Context, eval AuthzEvalContext) (DataScope, error)
}

// DataScopeResolverFunc 允许使用函数直接实现 IDataScopeResolver。
type DataScopeResolverFunc func(ctx context.Context, eval AuthzEvalContext) (DataScope, error)

// Resolve 解析当前数据边界。
func (f DataScopeResolverFunc) Resolve(ctx context.Context, eval AuthzEvalContext) (DataScope, error) {
	return f(ctx, eval)
}

// PrincipalDataScopeResolver 从 Principal 推导最小数据边界。
type PrincipalDataScopeResolver struct{}

// Resolve 根据主体信息推导 DataScope。
func (PrincipalDataScopeResolver) Resolve(ctx context.Context, eval AuthzEvalContext) (DataScope, error) {
	_ = ctx
	principal := eval.Principal.Clone()
	if principal.IsSystem && principal.ActiveScopeID == 0 {
		return DataScope{Mode: ScopeModeGlobal}, nil
	}
	if principal.ActiveScopeID == 0 {
		return DataScope{}, errors.NewCode(errors.InvalidInput, "active scope ID is required in principal")
	}
	return normalizeDataScope(DataScope{ActiveScopeID: principal.ActiveScopeID, VisibleScopeIDs: []int64{principal.ActiveScopeID}, Mode: ScopeModeScoped}), nil
}

// WithDataScope 将数据边界写入 context。
func WithDataScope(ctx context.Context, scope DataScope) (context.Context, error) {
	principalCtx, err := RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	return withDataScopeBinding(principalCtx, scope, false), nil
}

// DataScopeFromContext 从 context 中读取数据边界。
func DataScopeFromContext(ctx context.Context) (DataScope, bool) {
	if ctx == nil {
		return DataScope{}, false
	}
	switch binding := ctx.Value(dataScopeContextKey{}).(type) {
	case dataScopeBinding:
		if dataScopeIsEmpty(binding.Scope) {
			return DataScope{}, false
		}
		scope := normalizeDataScope(binding.Scope)
		return scope, hasDataScope(scope)
	case DataScope:
		if dataScopeIsEmpty(binding) {
			return DataScope{}, false
		}
		scope := normalizeDataScope(binding)
		return scope, hasDataScope(scope)
	default:
		return DataScope{}, false
	}
}

// ResolveDataScope 优先读取已绑定 scope，否则委托 resolver 计算。
func ResolveDataScope(ctx context.Context, resolver IDataScopeResolver) (DataScope, error) {
	if scope, ok := DataScopeFromContext(ctx); ok {
		return scope, nil
	}
	if resolver == nil {
		return DataScope{}, errors.NewCode(errors.InvalidInput, "data scope resolver is required")
	}
	eval, err := EvalContextFromContext(ctx)
	if err != nil {
		return DataScope{}, err
	}
	scope, err := resolver.Resolve(ctx, eval)
	if err != nil {
		return DataScope{}, err
	}
	return normalizeDataScope(scope), nil
}

// RequireContext 确保 context 非空。
func RequireContext(ctx context.Context) (context.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx, nil
}

type dataScopeContextKey struct{}

func withDataScopeBinding(ctx context.Context, scope DataScope, derived bool) context.Context {
	if ctx == nil {
		return nil
	}
	// 先检查原始 scope 是否为空（normalize 前），避免 normalizeDataScope 把空输入
	// 推导为 {Mode: ScopeModeGlobal}，导致读取侧 dataScopeIsEmpty 误判为非空。
	var boundScope DataScope
	if !dataScopeIsEmpty(scope) {
		boundScope = normalizeDataScope(scope)
	}
	ctx = context.WithValue(ctx, dataScopeContextKey{}, dataScopeBinding{
		Scope:     boundScope,
		IsDerived: derived,
	})
	// 同步投影到 domain/access，供 app/repo 层在不依赖 auth 包的前提下消费数据边界。
	// 使用 boundScope（已规范化）而非原始 scope，保证两侧存储完全一致。
	return bindAccessDataScope(ctx, boundScope, derived)
}

func withDerivedDataScope(ctx context.Context, scope DataScope) context.Context {
	return withDataScopeBinding(ctx, scope, true)
}

func dataScopeIsDerivedFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	binding, ok := ctx.Value(dataScopeContextKey{}).(dataScopeBinding)
	return ok && binding.IsDerived && !dataScopeIsEmpty(binding.Scope)
}

func normalizeDataScope(scope DataScope) DataScope {
	scope.ActiveScopeID = NormalizePositiveID(scope.ActiveScopeID)
	scope.VisibleScopeIDs = normalizeIDSlice(scope.VisibleScopeIDs)
	scope.TenantIDs = normalizeIDSlice(scope.TenantIDs)
	if len(scope.VisibleScopeIDs) == 0 && scope.ActiveScopeID > 0 {
		scope.VisibleScopeIDs = []int64{scope.ActiveScopeID}
	}
	if scope.ActiveScopeID == 0 && len(scope.VisibleScopeIDs) == 1 {
		scope.ActiveScopeID = scope.VisibleScopeIDs[0]
	}
	if scope.Mode == "" {
		switch {
		case scope.ActiveScopeID > 0 || len(scope.VisibleScopeIDs) > 0 || len(scope.TenantIDs) > 0:
			scope.Mode = ScopeModeScoped
		default:
			scope.Mode = ScopeModeGlobal
		}
	}
	if scope.Mode == ScopeModeGlobal {
		scope.ActiveScopeID = 0
		scope.VisibleScopeIDs = nil
	}
	return scope
}

func hasDataScope(scope DataScope) bool {
	return scope.ActiveScopeID > 0 || len(scope.VisibleScopeIDs) > 0 || len(scope.TenantIDs) > 0 || scope.Mode != ""
}

func dataScopeIsEmpty(scope DataScope) bool {
	if scope.Mode != "" || NormalizePositiveID(scope.ActiveScopeID) > 0 {
		return false
	}
	for _, value := range scope.VisibleScopeIDs {
		if NormalizePositiveID(value) > 0 {
			return false
		}
	}
	for _, value := range scope.TenantIDs {
		if NormalizePositiveID(value) > 0 {
			return false
		}
	}
	return true
}

func normalizeIDSlice(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		value = NormalizePositiveID(value)
		if value == 0 {
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
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// NormalizePositiveID 在 value 为正数时返回原值，否则返回 0。
func NormalizePositiveID(value int64) int64 {
	if value <= 0 {
		return 0
	}
	return value
}
