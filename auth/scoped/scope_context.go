package scoped

import (
	"context"

	"gochen/errors"
)

// IDataScopeResolver 解析当前请求的数据范围。
//
// 错误契约：无法确定范围时必须返回错误，严禁返回零值让调用方自行解释。
type IDataScopeResolver interface {
	ResolveDataScope(ctx context.Context) (DataScope, error)
}

// DataScopeResolverFunc 允许用函数直接实现 IDataScopeResolver。
type DataScopeResolverFunc func(ctx context.Context) (DataScope, error)

// ResolveDataScope 解析当前数据范围。
func (f DataScopeResolverFunc) ResolveDataScope(ctx context.Context) (DataScope, error) {
	if f == nil {
		return DenyAll(), errors.NewCode(
			errors.InvalidInput,
			"data scope resolver function cannot be nil; this indicates a wiring bug",
		)
	}
	return f(ctx)
}

type dataScopeContextKey struct{}

// WithDataScope 将数据范围绑定到 context。
func WithDataScope(ctx context.Context, scope DataScope) (context.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return context.WithValue(ctx, dataScopeContextKey{}, scope.Normalize()), nil
}

// DataScopeFromContext 从 context 读取数据范围。
func DataScopeFromContext(ctx context.Context) (DataScope, bool) {
	if ctx == nil {
		return DenyAll(), false
	}
	scope, ok := ctx.Value(dataScopeContextKey{}).(DataScope)
	if !ok {
		return DenyAll(), false
	}
	return scope, true
}

// ResolveDataScope 优先读取已绑定范围，否则委托 resolver 计算。
//
// 两者都不可用时返回 ScopeDenyAll 与错误——绝不默认放行。
func ResolveDataScope(ctx context.Context, resolver IDataScopeResolver) (DataScope, error) {
	if scope, ok := DataScopeFromContext(ctx); ok {
		return scope, nil
	}
	if resolver == nil {
		return DenyAll(), errors.NewCode(errors.Forbidden, "data scope resolver is required")
	}
	scope, err := resolver.ResolveDataScope(ctx)
	if err != nil {
		return DenyAll(), err
	}
	return scope.Normalize(), nil
}
