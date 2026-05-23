package access

import (
	"context"
	"sort"

	"gochen/errors"
)

// ScopeMode 表示默认数据边界的约束方式。
type ScopeMode string

const (
	// ScopeModeGlobal 表示当前请求不受数据边界限制。
	ScopeModeGlobal ScopeMode = "global"
	// ScopeModeScoped 表示当前请求仅能访问指定 scope 集合。
	ScopeModeScoped ScopeMode = "scoped"
)

// DataScope 表达 repo / orm 层消费的默认数据边界。
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

// IDataScopeResolver 定义默认数据边界解析器。
type IDataScopeResolver interface {
	ResolveDataScope(ctx context.Context) (DataScope, error)
}

// DataScopeResolverFunc 允许用函数直接实现 IDataScopeResolver。
type DataScopeResolverFunc func(ctx context.Context) (DataScope, error)

// ResolveDataScope 解析当前数据边界。
func (f DataScopeResolverFunc) ResolveDataScope(ctx context.Context) (DataScope, error) {
	return f(ctx)
}

// ContextDataScopeResolver 仅从 context 中读取已绑定数据边界。
type ContextDataScopeResolver struct{}

// ResolveDataScope 从 context 中读取数据边界。
func (ContextDataScopeResolver) ResolveDataScope(ctx context.Context) (DataScope, error) {
	scope, ok := DataScopeFromContext(ctx)
	if !ok {
		return DataScope{}, errors.NewCode(errors.InvalidInput, "data scope is required")
	}
	return scope, nil
}

type dataScopeContextKey struct{}

// WithDataScope 将默认数据边界绑定到 context。
func WithDataScope(ctx context.Context, scope DataScope) context.Context {
	return withDataScopeBinding(ctx, scope, false)
}

// WithDerivedDataScope 将“由运行时主体推导出的”数据边界绑定到 context。
func WithDerivedDataScope(ctx context.Context, scope DataScope) context.Context {
	return withDataScopeBinding(ctx, scope, true)
}

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
	return context.WithValue(ctx, dataScopeContextKey{}, dataScopeBinding{
		Scope:     boundScope,
		IsDerived: derived,
	})
}

// DataScopeFromContext 从 context 中读取默认数据边界。
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

// DataScopeIsDerivedFromContext 返回当前绑定的 data scope 是否为运行时推导值。
func DataScopeIsDerivedFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	binding, ok := ctx.Value(dataScopeContextKey{}).(dataScopeBinding)
	return ok && binding.IsDerived && !dataScopeIsEmpty(binding.Scope)
}

// ResolveDataScope 优先读取已绑定 scope，否则委托 resolver 解析。
func ResolveDataScope(ctx context.Context, resolver IDataScopeResolver) (DataScope, error) {
	if scope, ok := DataScopeFromContext(ctx); ok {
		return scope, nil
	}
	if resolver == nil {
		return DataScope{}, errors.NewCode(errors.InvalidInput, "data scope resolver is required")
	}
	scope, err := resolver.ResolveDataScope(ctx)
	if err != nil {
		return DataScope{}, err
	}
	return normalizeDataScope(scope), nil
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
