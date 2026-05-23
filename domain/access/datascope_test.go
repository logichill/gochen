package access

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizePositiveID(t *testing.T) {
	require.Equal(t, int64(0), NormalizePositiveID(0))
	require.Equal(t, int64(0), NormalizePositiveID(-1))
	require.Equal(t, int64(1), NormalizePositiveID(1))
	require.Equal(t, int64(42), NormalizePositiveID(42))
}

func TestDataScope_Normalize(t *testing.T) {
	t.Run("global mode clears IDs", func(t *testing.T) {
		scope := normalizeDataScope(DataScope{ActiveScopeID: 5, Mode: ScopeModeGlobal})
		require.Equal(t, int64(0), scope.ActiveScopeID)
		require.Nil(t, scope.VisibleScopeIDs)
	})

	t.Run("infers managed scopes mode from ActiveScopeID", func(t *testing.T) {
		scope := normalizeDataScope(DataScope{ActiveScopeID: 10})
		require.Equal(t, ScopeModeScoped, scope.Mode)
		require.Equal(t, int64(10), scope.ActiveScopeID)
		require.Equal(t, []int64{10}, scope.VisibleScopeIDs)
	})

	t.Run("infers ActiveScopeID from single VisibleScopeIDs", func(t *testing.T) {
		scope := normalizeDataScope(DataScope{VisibleScopeIDs: []int64{7}})
		require.Equal(t, int64(7), scope.ActiveScopeID)
		require.Equal(t, ScopeModeScoped, scope.Mode)
	})

	t.Run("deduplicates and sorts VisibleScopeIDs", func(t *testing.T) {
		scope := normalizeDataScope(DataScope{VisibleScopeIDs: []int64{5, 3, 5, 0, -1, 2}})
		require.Equal(t, []int64{2, 3, 5}, scope.VisibleScopeIDs)
	})

	t.Run("empty scope defaults to global", func(t *testing.T) {
		scope := normalizeDataScope(DataScope{})
		require.Equal(t, ScopeModeGlobal, scope.Mode)
	})
}

func TestDataScope_ContextRoundTrip(t *testing.T) {
	scope := DataScope{ActiveScopeID: 1, VisibleScopeIDs: []int64{1, 2}, Mode: ScopeModeScoped}
	ctx := WithDataScope(context.Background(), scope)
	read, ok := DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, int64(1), read.ActiveScopeID)
	require.Equal(t, []int64{1, 2}, read.VisibleScopeIDs)
	require.Equal(t, ScopeModeScoped, read.Mode)
	require.False(t, DataScopeIsDerivedFromContext(ctx))
}

func TestDataScope_DerivedContextRoundTrip(t *testing.T) {
	scope := DataScope{ActiveScopeID: 7, VisibleScopeIDs: []int64{7}, Mode: ScopeModeScoped}
	ctx := WithDerivedDataScope(context.Background(), scope)
	read, ok := DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, int64(7), read.ActiveScopeID)
	require.True(t, DataScopeIsDerivedFromContext(ctx))
}

func TestDataScope_EmptyDerivedContextDoesNotBecomeGlobal(t *testing.T) {
	ctx := WithDerivedDataScope(context.Background(), DataScope{})
	_, ok := DataScopeFromContext(ctx)
	require.False(t, ok)
	require.False(t, DataScopeIsDerivedFromContext(ctx))
}

func TestDataScope_InvalidDerivedContextDoesNotBecomeGlobal(t *testing.T) {
	ctx := WithDerivedDataScope(context.Background(), DataScope{
		ActiveScopeID:   -1,
		VisibleScopeIDs: []int64{0, -2},
		TenantIDs:       []int64{-3},
	})
	_, ok := DataScopeFromContext(ctx)
	require.False(t, ok)
	require.False(t, DataScopeIsDerivedFromContext(ctx))
}

func TestDataScope_EmptyExplicitContextDoesNotBecomeGlobal(t *testing.T) {
	// 显式绑定空 DataScope 不应被读出为 Global scope——
	// 与 derived 空 scope 的语义保持一致：无有效 scope 信息则不应返回。
	ctx := WithDataScope(context.Background(), DataScope{})
	_, ok := DataScopeFromContext(ctx)
	require.False(t, ok)
	require.False(t, DataScopeIsDerivedFromContext(ctx))
}

func TestDataScope_ExplicitGlobalScopeIsReadable(t *testing.T) {
	// 显式绑定包含 Mode 字段的 Global scope 应能被正常读出。
	ctx := WithDataScope(context.Background(), DataScope{Mode: ScopeModeGlobal})
	read, ok := DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, ScopeModeGlobal, read.Mode)
	require.Zero(t, read.ActiveScopeID)
	require.Empty(t, read.VisibleScopeIDs)
	require.False(t, DataScopeIsDerivedFromContext(ctx))
}

func TestResolveDataScope_PrefersContext(t *testing.T) {
	ctxScope := DataScope{ActiveScopeID: 10, Mode: ScopeModeScoped}
	ctx := WithDataScope(context.Background(), ctxScope)
	resolved, err := ResolveDataScope(ctx, ContextDataScopeResolver{})
	require.NoError(t, err)
	require.Equal(t, int64(10), resolved.ActiveScopeID)
}

func TestResolveDataScope_FallsBackToResolver(t *testing.T) {
	resolver := DataScopeResolverFunc(func(ctx context.Context) (DataScope, error) {
		return DataScope{ActiveScopeID: 99, Mode: ScopeModeScoped}, nil
	})
	resolved, err := ResolveDataScope(context.Background(), resolver)
	require.NoError(t, err)
	require.Equal(t, int64(99), resolved.ActiveScopeID)
}

func TestResolveDataScope_NilResolverReturnsError(t *testing.T) {
	_, err := ResolveDataScope(context.Background(), nil)
	require.Error(t, err)
}

func TestContextDataScopeResolver_MissingScopeReturnsError(t *testing.T) {
	_, err := ContextDataScopeResolver{}.ResolveDataScope(context.Background())
	require.Error(t, err)
}
