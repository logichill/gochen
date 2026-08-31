// Package scoped 提供 L3 安全能力的**契约**：主体在什么数据范围内、能对哪条资源做什么。
//
// 设计边界：
//   - 本包只有契约与纯值对象，物理执行由具体 Repo 在构造期显式声明安全列完成；
//   - PEP 编排位于 gochen/app/security/scoped，本包不依赖任何 app 包；
//   - 取代原 gochen/domain/access 与 gochen/auth 根包中的范围/资源/决策资产。
package scoped

import (
	"context"
	"sort"
	"strings"

	"gochen/errors"
)

// DataScopeKind 表达数据范围的三种状态。
//
// 三态建模是为了消除"空值歧义"：旧模型只有 Global / Scoped 两态，
// "空的可见范围列表 + Scoped" 既可能表示"什么都看不到"，
// 也可能被误解释为"没有限制"。显式的 ScopeDenyAll 消除该歧义。
type DataScopeKind int

const (
	// ScopeDenyAll 明确拒绝：没有任何可见范围 → 读写路径均返回 Forbidden。
	//
	// 注意这是 DataScope 的**零值**，因此未初始化的范围默认 fail-closed。
	ScopeDenyAll DataScopeKind = iota
	// ScopeFiltered 限定在具体范围集合内 → WHERE managed_scope_id IN (...)。
	ScopeFiltered
	// ScopeGlobal 显式全局可见（超级管理员/系统服务）→ 不追加范围过滤。
	//
	// 只能由框架在严格受控的服务上下文中设置，普通请求不可触达。
	ScopeGlobal
)

// String 返回可读的范围状态名，用于日志与错误上下文。
func (k DataScopeKind) String() string {
	switch k {
	case ScopeFiltered:
		return "filtered"
	case ScopeGlobal:
		return "global"
	default:
		return "deny_all"
	}
}

// DataScope 表达一次请求可见/可操作的数据边界。
type DataScope struct {
	// Kind 范围状态；零值为 ScopeDenyAll（fail-closed）。
	Kind DataScopeKind
	// ScopeIDs 仅在 Kind == ScopeFiltered 时有效。
	//
	// 保持 int64 与既有 managed_scope_id 列类型及下游存量数据一致（裁定 8）。
	ScopeIDs []int64
	// TenantID 隔离空间标识，与 contextx.TenantID / ITenantEntity 一致为 string。
	TenantID string
}

// Filtered 构造一个限定在给定范围集合内的 DataScope。
//
// 传入空集合会得到 ScopeDenyAll——"限定在空集合内"语义上等价于什么都看不到，
// 绝不退化为不加过滤。
func Filtered(scopeIDs ...int64) DataScope {
	normalized := normalizeScopeIDs(scopeIDs)
	if len(normalized) == 0 {
		return DataScope{Kind: ScopeDenyAll}
	}
	return DataScope{Kind: ScopeFiltered, ScopeIDs: normalized}
}

// Global 构造一个显式全局可见的 DataScope。
func Global() DataScope { return DataScope{Kind: ScopeGlobal} }

// DenyAll 构造一个明确拒绝的 DataScope。
func DenyAll() DataScope { return DataScope{Kind: ScopeDenyAll} }

// Normalize 返回规范化后的数据范围。
//
// 规范化会去重、排序、剔除非正数 ID；ScopeFiltered 但无有效 ID 时收敛为
// ScopeDenyAll，ScopeGlobal 时清空范围列表避免歧义。
func (s DataScope) Normalize() DataScope {
	switch s.Kind {
	case ScopeGlobal:
		s.ScopeIDs = nil
		return s
	case ScopeFiltered:
		s.ScopeIDs = normalizeScopeIDs(s.ScopeIDs)
		if len(s.ScopeIDs) == 0 {
			s.Kind = ScopeDenyAll
		}
		return s
	default:
		s.Kind = ScopeDenyAll
		s.ScopeIDs = nil
		return s
	}
}

// AllowsAny 判断该范围是否允许访问任何数据。
func (s DataScope) AllowsAny() bool {
	normalized := s.Normalize()
	return normalized.Kind == ScopeGlobal ||
		(normalized.Kind == ScopeFiltered && len(normalized.ScopeIDs) > 0)
}

// Contains 判断指定 scope ID 是否落在可见范围内。
func (s DataScope) Contains(scopeID int64) bool {
	normalized := s.Normalize()
	switch normalized.Kind {
	case ScopeGlobal:
		return true
	case ScopeFiltered:
		for _, id := range normalized.ScopeIDs {
			if id == scopeID {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// Intersect 返回两个数据范围的交集。
//
// 用于把不同来源的范围叠加：PDP 决策回传的授权范围与主体自身的可见范围
// 必须**取交**而不是互相覆盖（§4.1 第 3 条"约束只增不减"）。
// 任一侧拒绝即整体拒绝；ScopeGlobal 是交运算的单位元（不收窄对方）。
//
// TenantID 随交集一起传播：一侧为空时继承另一侧；两侧非空且不一致说明
// 这两个范围分属不同隔离空间，没有任何共享数据，直接收敛为 ScopeDenyAll
// ——任选一侧都等于把边界悄悄放大到另一侧。
func (s DataScope) Intersect(other DataScope) DataScope {
	left, right := s.Normalize(), other.Normalize()
	if left.Kind == ScopeDenyAll || right.Kind == ScopeDenyAll {
		return DenyAll()
	}
	tenant, sameTenant := intersectTenantID(left.TenantID, right.TenantID)
	if !sameTenant {
		return DenyAll()
	}
	if left.Kind == ScopeGlobal {
		right.TenantID = tenant
		return right
	}
	if right.Kind == ScopeGlobal {
		left.TenantID = tenant
		return left
	}
	allowed := make(map[int64]struct{}, len(right.ScopeIDs))
	for _, id := range right.ScopeIDs {
		allowed[id] = struct{}{}
	}
	shared := make([]int64, 0, len(left.ScopeIDs))
	for _, id := range left.ScopeIDs {
		if _, ok := allowed[id]; ok {
			shared = append(shared, id)
		}
	}
	// Filtered 交集为空即"什么都看不到"，Filtered 会自动收敛为 ScopeDenyAll。
	result := Filtered(shared...)
	result.TenantID = tenant
	return result
}

// intersectTenantID 归并两个范围的隔离归属。
//
// 一侧为空表示该侧未声明隔离边界，取另一侧；两侧非空且相同则保留；
// 两侧非空且不同返回 false——调用方应视为交集为空。
func intersectTenantID(left, right string) (string, bool) {
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)
	switch {
	case left == "":
		return right, true
	case right == "":
		return left, true
	case left == right:
		return left, true
	default:
		return "", false
	}
}

func normalizeScopeIDs(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
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
