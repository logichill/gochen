package scoped

import (
	"context"
	"strings"
)

// 受控约束通道。
//
// 约束既不进入任何 Application 公共接口，也不在 ctx 中裸存数据结构：
//
//	app/security/scoped 装饰器
//	    │  PDP 判定通过后构造 IConstraintProvider
//	    ▼
//	scoped.WithConstraint(ctx, provider)   ← 唯一写入口，派生一次性 ctx
//	    │  基础 Application（crud/audited/eventsourced）对此完全无感知
//	    ▼
//	scoped.ConstraintFrom(ctx, entityType) ← Repo 在写路径 fail-closed 读取
//
// 之所以按实体类型投放而不是全局塞一份：Hook 内写其他实体（如操作日志）时
// 匹配不到约束，L3 严格模式下应当拒绝而不是拿着订单的约束去写日志。

// IConstraintProvider 按目标实体类型精确投放写约束。
//
// 匹配不到时必须返回 ok=false，由调用方 fail-closed 处理，
// 严禁返回空约束让调用方误解释为"无限制"。
type IConstraintProvider interface {
	ConstraintFor(entityType string) (WriteConstraint, bool)
}

// ConstraintProviderFunc 允许用函数直接实现 IConstraintProvider。
type ConstraintProviderFunc func(entityType string) (WriteConstraint, bool)

// ConstraintFor 按实体类型返回写约束。
func (f ConstraintProviderFunc) ConstraintFor(entityType string) (WriteConstraint, bool) {
	if f == nil {
		return WriteConstraint{}, false
	}
	return f(entityType)
}

// SingleEntityConstraint 构造只对单一实体类型生效的约束提供者。
//
// 这是最常见的形态：一次 CRUD 写操作只授权一种实体。
func SingleEntityConstraint(entityType string, constraint WriteConstraint) IConstraintProvider {
	target := normalizeEntityType(entityType)
	normalized := constraint.Normalize()
	return ConstraintProviderFunc(func(requested string) (WriteConstraint, bool) {
		if normalizeEntityType(requested) != target {
			return WriteConstraint{}, false
		}
		return normalized, true
	})
}

type constraintContextKey struct{}

// WithConstraint 是写约束进入调用链的**唯一入口**。
//
// 由 app/security/scoped 装饰器在 PDP 判定后调用，返回一次性派生 ctx；
// 调用链结束即弃，不跨请求残留。
//
// provider 为 nil 时原样返回 ctx——不写入即意味着下游读不到约束，
// 从而在写路径 fail-closed，这是安全的默认。
func WithConstraint(ctx context.Context, provider IConstraintProvider) context.Context {
	if ctx == nil || provider == nil {
		return ctx
	}
	return context.WithValue(ctx, constraintContextKey{}, provider)
}

// ConstraintFrom 由 Repo 在写路径以 fail-closed 方式读取约束。
//
// 返回 ok=false 的场景（全部必须由调用方拒绝，绝不放行）：
//   - ctx 中没有约束提供者（未经 L3 装饰器，或异步丢失了派生 ctx）；
//   - 提供者不认识该实体类型（Hook 内写了其他实体）。
func ConstraintFrom(ctx context.Context, entityType string) (WriteConstraint, bool) {
	if ctx == nil {
		return WriteConstraint{}, false
	}
	provider, ok := ctx.Value(constraintContextKey{}).(IConstraintProvider)
	if !ok || provider == nil {
		return WriteConstraint{}, false
	}
	constraint, ok := provider.ConstraintFor(entityType)
	if !ok {
		return WriteConstraint{}, false
	}
	// 空约束等价于"没有授权任何资源"，同样按未命中处理。
	normalized := constraint.Normalize()
	if normalized.IsEmpty() {
		return WriteConstraint{}, false
	}
	return normalized, true
}

// HasConstraintProvider 判断 ctx 是否携带约束提供者。
//
// 仅用于诊断（区分"未经 L3 装饰"与"装饰了但实体类型不匹配"），
// 不得用于放行判定。
func HasConstraintProvider(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	provider, ok := ctx.Value(constraintContextKey{}).(IConstraintProvider)
	return ok && provider != nil
}

func normalizeEntityType(entityType string) string {
	return strings.ToLower(strings.TrimSpace(entityType))
}
