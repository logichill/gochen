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
