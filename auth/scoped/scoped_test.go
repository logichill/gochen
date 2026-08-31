package scoped_test

import (
	"context"
	"testing"

	"gochen/auth/scoped"
	"gochen/errors"
	"gochen/testkit/require"
)

// --- 三态 DataScope ---

// 零值必须是 DenyAll——未初始化的范围默认 fail-closed，而不是"无限制"。
func TestDataScopeZeroValueIsDenyAll(t *testing.T) {
	var scope scoped.DataScope
	require.Equal(t, scoped.ScopeDenyAll, scope.Kind)
	require.False(t, scope.AllowsAny())
	require.False(t, scope.Contains(1))
}

// "限定在空集合内"必须收敛为 DenyAll，绝不退化为不加过滤。
func TestFilteredWithNoIDsBecomesDenyAll(t *testing.T) {
	require.Equal(t, scoped.ScopeDenyAll, scoped.Filtered().Kind)
	require.Equal(t, scoped.ScopeDenyAll, scoped.Filtered(0, -1).Kind)
	require.False(t, scoped.Filtered().AllowsAny())
}

func TestFilteredNormalizesIDs(t *testing.T) {
	scope := scoped.Filtered(3, 1, 3, 0, -5, 2)
	require.Equal(t, scoped.ScopeFiltered, scope.Kind)
	require.Equal(t, []int64{1, 2, 3}, scope.ScopeIDs)

	require.True(t, scope.Contains(2))
	require.False(t, scope.Contains(9))
	require.True(t, scope.AllowsAny())
}

func TestGlobalScopeContainsEverything(t *testing.T) {
	scope := scoped.Global()
	require.True(t, scope.AllowsAny())
	require.True(t, scope.Contains(12345))
	require.Empty(t, scope.Normalize().ScopeIDs, "global 不应携带范围列表")
}

func TestDataScopeKindString(t *testing.T) {
	require.Equal(t, "deny_all", scoped.ScopeDenyAll.String())
	require.Equal(t, "filtered", scoped.ScopeFiltered.String())
	require.Equal(t, "global", scoped.ScopeGlobal.String())
}

// --- Intersect：决策范围与主体范围求交（§4.1 约束只增不减）---

func TestIntersectNarrowsToSharedIDs(t *testing.T) {
	left := scoped.Filtered(1, 2, 3)
	right := scoped.Filtered(2, 3, 4)

	got := left.Intersect(right)
	require.Equal(t, scoped.ScopeFiltered, got.Kind)
	require.Equal(t, []int64{2, 3}, got.ScopeIDs)
}

func TestIntersectEmptyIntersectionBecomesDenyAll(t *testing.T) {
	got := scoped.Filtered(1).Intersect(scoped.Filtered(2))
	require.Equal(t, scoped.ScopeDenyAll, got.Kind)

	got = scoped.Filtered(1).Intersect(scoped.DenyAll())
	require.Equal(t, scoped.ScopeDenyAll, got.Kind)
}

func TestIntersectGlobalIsIdentity(t *testing.T) {
	filtered := scoped.Filtered(5)

	got := scoped.Global().Intersect(filtered)
	require.Equal(t, scoped.ScopeFiltered, got.Kind)
	require.Equal(t, []int64{5}, got.ScopeIDs)

	got = filtered.Intersect(scoped.Global())
	require.Equal(t, scoped.ScopeFiltered, got.Kind)
	require.Equal(t, []int64{5}, got.ScopeIDs)
}

// 交集必须继承隔离归属：一侧未声明租户时取另一侧，两侧一致时保留。
// 丢失该字段会让依赖交集 TenantID 的下游静默失去租户归属。
func TestIntersectPropagatesTenantID(t *testing.T) {
	tenantScope := scoped.Filtered(1)
	tenantScope.TenantID = "tenant-a"

	got := scoped.Filtered(1, 2).Intersect(tenantScope)
	require.Equal(t, "tenant-a", got.TenantID)

	got = tenantScope.Intersect(scoped.Filtered(1, 2))
	require.Equal(t, "tenant-a", got.TenantID)

	both := scoped.Filtered(1)
	both.TenantID = "tenant-a"
	got = both.Intersect(tenantScope)
	require.Equal(t, "tenant-a", got.TenantID)
}

// 两侧隔离归属不同：没有任何共享数据，交集必须是 DenyAll，
// 而不是任选一侧——那等于把边界悄悄放大到另一侧。
func TestIntersectConflictingTenantsDeniesAll(t *testing.T) {
	left := scoped.Filtered(1)
	left.TenantID = "tenant-a"
	right := scoped.Filtered(1)
	right.TenantID = "tenant-b"

	got := left.Intersect(right)
	require.Equal(t, scoped.ScopeDenyAll, got.Kind)
	require.False(t, got.AllowsAny())
}

func TestDataScopeContextRoundTrip(t *testing.T) {
	ctx, err := scoped.WithDataScope(context.Background(), scoped.Filtered(7))
	require.NoError(t, err)

	got, ok := scoped.DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, scoped.ScopeFiltered, got.Kind)
	require.Equal(t, []int64{7}, got.ScopeIDs)

	_, ok = scoped.DataScopeFromContext(context.Background())
	require.False(t, ok)
}

// 既无绑定范围也无 resolver → DenyAll + 错误，绝不默认放行。
func TestResolveDataScopeFailsClosed(t *testing.T) {
	scope, err := scoped.ResolveDataScope(context.Background(), nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
	require.Equal(t, scoped.ScopeDenyAll, scope.Kind)
}

func TestResolveDataScopePrefersBoundScope(t *testing.T) {
	ctx, err := scoped.WithDataScope(context.Background(), scoped.Filtered(5))
	require.NoError(t, err)

	resolver := scoped.DataScopeResolverFunc(func(context.Context) (scoped.DataScope, error) {
		return scoped.Global(), nil
	})
	scope, err := scoped.ResolveDataScope(ctx, resolver)
	require.NoError(t, err)
	require.Equal(t, scoped.ScopeFiltered, scope.Kind, "已绑定范围优先于 resolver")
}

// --- 受控约束通道 ---

func TestConstraintChannelRoundTrip(t *testing.T) {
	constraint := scoped.WriteConstraint{Resources: []scoped.ResourceConstraint{{
		Kind: "order", ResourceID: "1", ManagedScopeID: 101, Revision: "5",
	}}}
	provider := scoped.SingleEntityConstraint("order", constraint)

	ctx := scoped.WithConstraint(context.Background(), provider)
	got, ok := scoped.ConstraintFrom(ctx, "order")
	require.True(t, ok)
	require.Equal(t, 1, len(got.Resources))
	require.Equal(t, int64(101), got.Resources[0].ManagedScopeID)

	// 大小写与空白不敏感
	_, ok = scoped.ConstraintFrom(ctx, "  ORDER ")
	require.True(t, ok)
}

// 未经 L3 装饰器的 ctx 读不到约束 → 写路径必须 fail-closed。
func TestConstraintFromWithoutProviderFailsClosed(t *testing.T) {
	_, ok := scoped.ConstraintFrom(context.Background(), "order")
	require.False(t, ok)
	require.False(t, scoped.HasConstraintProvider(context.Background()))
}

// 嵌套写防护：Hook 内写其他实体时匹配不到约束，必须拒绝而不是复用订单的约束。
func TestConstraintFromRejectsUnknownEntityType(t *testing.T) {
	provider := scoped.SingleEntityConstraint("order", scoped.WriteConstraint{
		Resources: []scoped.ResourceConstraint{{Kind: "order", ResourceID: "1"}},
	})
	ctx := scoped.WithConstraint(context.Background(), provider)

	_, ok := scoped.ConstraintFrom(ctx, "audit_log")
	require.False(t, ok, "其他实体类型不得命中订单的约束")

	// 但能区分"未装饰"与"装饰了但类型不匹配"，便于诊断
	require.True(t, scoped.HasConstraintProvider(ctx))
}

// 空约束等价于"没有授权任何资源"，同样按未命中处理。
func TestConstraintFromTreatsEmptyConstraintAsMiss(t *testing.T) {
	provider := scoped.SingleEntityConstraint("order", scoped.WriteConstraint{})
	ctx := scoped.WithConstraint(context.Background(), provider)

	_, ok := scoped.ConstraintFrom(ctx, "order")
	require.False(t, ok)
}

// nil provider 不写入 ctx → 下游读不到 → fail-closed（安全默认）。
func TestWithConstraintIgnoresNilProvider(t *testing.T) {
	ctx := scoped.WithConstraint(context.Background(), nil)
	require.False(t, scoped.HasConstraintProvider(ctx))
}

// --- 决策与约束投影 ---

// allow 但没有授权资源不算允许，避免"空 allow"成为通行证。
func TestEmptyAllowIsNotAllowed(t *testing.T) {
	require.False(t, scoped.Allow().IsAllowed())
	require.True(t, scoped.Allow(scoped.Resource{Kind: "order", ID: "1"}).IsAllowed())
	require.False(t, scoped.Deny(scoped.ReasonOutOfScope).IsAllowed())
}

func TestDecisionProjectsWriteConstraint(t *testing.T) {
	decision := scoped.Allow(scoped.Resource{
		Kind: "order", ID: "1", ManagedScopeID: 101, TenantID: "tenant-a", Revision: "5",
	})
	constraint := decision.WriteConstraint()
	require.Equal(t, 1, len(constraint.Resources))

	resource, ok := constraint.Single()
	require.True(t, ok)
	require.Equal(t, "order", resource.Kind)
	require.Equal(t, int64(101), resource.ManagedScopeID)
	require.Equal(t, "tenant-a", resource.TenantID)
}

// deny 决策必须投影为空约束，下游据此拒绝。
func TestDenyDecisionProjectsEmptyConstraint(t *testing.T) {
	constraint := scoped.Deny(scoped.ReasonOutOfScope).WriteConstraint()
	require.True(t, constraint.IsEmpty())
	require.Equal(t, scoped.ScopeDenyAll, constraint.DataScope().Kind)
	require.Equal(t, scoped.ScopeDenyAll, scoped.Deny("x").DataScope().Kind)
}

func TestWriteConstraintDataScopeProjection(t *testing.T) {
	scopedConstraint := scoped.Allow(
		scoped.Resource{Kind: "order", ID: "1", ManagedScopeID: 101},
		scoped.Resource{Kind: "order", ID: "2", ManagedScopeID: 202},
	).WriteConstraint()
	scope := scopedConstraint.DataScope()
	require.Equal(t, scoped.ScopeFiltered, scope.Kind)
	require.Equal(t, []int64{101, 202}, scope.ScopeIDs)

	// 任一资源是全局资源 → 整体全局
	globalConstraint := scoped.Allow(
		scoped.Resource{Kind: "order", ID: "1", GlobalScope: true},
	).WriteConstraint()
	require.Equal(t, scoped.ScopeGlobal, globalConstraint.DataScope().Kind)
}

// GlobalScope 资源不得同时携带 managed scope，避免边界歧义。
func TestResourceNormalizeClearsScopeWhenGlobal(t *testing.T) {
	resource := scoped.Resource{Kind: " order ", ID: " 1 ", ManagedScopeID: 101, GlobalScope: true}.Normalize()
	require.Equal(t, "order", resource.Kind)
	require.Equal(t, "1", resource.ID)
	require.Equal(t, int64(0), resource.ManagedScopeID)
}

// --- Authorizer ---

func TestNewAuthorizerRejectsNilEvaluator(t *testing.T) {
	_, err := scoped.NewAuthorizer(nil, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestAuthorizerRequireDeniesOnDenyDecision(t *testing.T) {
	evaluator := scoped.EvaluatorFunc(func(context.Context, string, []scoped.Resource) (scoped.Decision, error) {
		return scoped.Deny(scoped.ReasonOutOfScope), nil
	})
	authorizer, err := scoped.NewAuthorizer(nil, evaluator)
	require.NoError(t, err)

	err = authorizer.Require(context.Background(), "order:api:read", scoped.Resource{Kind: "order", ID: "1"})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

func TestAuthorizerAllowsAndReturnsResources(t *testing.T) {
	evaluator := scoped.EvaluatorFunc(func(_ context.Context, _ string, resources []scoped.Resource) (scoped.Decision, error) {
		return scoped.Allow(resources...), nil
	})
	authorizer, err := scoped.NewAuthorizer(nil, evaluator)
	require.NoError(t, err)

	decision, err := authorizer.Authorize(context.Background(), "order:api:read",
		scoped.Resource{Kind: "order", ID: "1", ManagedScopeID: 101})
	require.NoError(t, err)
	require.True(t, decision.IsAllowed())
	require.Equal(t, 1, len(decision.AuthorizedResources))

	require.NoError(t, authorizer.Require(context.Background(), "order:api:read",
		scoped.Resource{Kind: "order", ID: "1"}))
}

// 空 action 必须拒绝。
func TestAuthorizerRejectsEmptyAction(t *testing.T) {
	evaluator := scoped.EvaluatorFunc(func(context.Context, string, []scoped.Resource) (scoped.Decision, error) {
		return scoped.Allow(scoped.Resource{Kind: "order"}), nil
	})
	authorizer, err := scoped.NewAuthorizer(nil, evaluator)
	require.NoError(t, err)

	_, err = authorizer.Authorize(context.Background(), "  ")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// 无法解析的目标必须报错，绝不忽略失败项。
func TestResolveResourcesFailsOnUnresolvableTarget(t *testing.T) {
	_, err := scoped.ResolveResources(nil, struct{ X int }{X: 1})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestResourceRegistryResolvesInOrder(t *testing.T) {
	type order struct{ ID string }
	registry := scoped.NewResourceRegistry(
		scoped.TypedResourceResolver[*order](func(o *order) (scoped.Resource, bool) {
			return scoped.Resource{Kind: "order", ID: o.ID}, o != nil
		}),
	)
	resource, ok := registry.Resolve(&order{ID: "7"})
	require.True(t, ok)
	require.Equal(t, "order", resource.Kind)
	require.Equal(t, "7", resource.ID)

	_, ok = registry.Resolve("not-an-order")
	require.False(t, ok)
}
