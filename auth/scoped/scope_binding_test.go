package scoped_test

import (
	"context"
	"testing"

	"gochen/auth"
	"gochen/auth/scoped"
	"gochen/contextx"
	"gochen/errors"
	"gochen/testkit/require"
)

func TestDataScopeBindingInvalidatesIdentityChangesAndReturnTrips(t *testing.T) {
	for _, kind := range []string{"subject", "scope", "tenant", "system"} {
		t.Run(kind, func(t *testing.T) {
			ctx, err := contextx.WithTenantID(context.Background(), "A")
			require.NoError(t, err)
			identity := auth.Principal{SubjectID: "alice", TenantID: "A", ScopeID: 7}
			ctx, err = auth.WithPrincipal(ctx, identity)
			require.NoError(t, err)
			ctx, err = scoped.WithDataScope(ctx, scoped.Global())
			require.NoError(t, err)
			changed := identity
			switch kind {
			case "subject":
				changed.SubjectID = "bob"
			case "scope":
				changed.ScopeID = 99
			case "tenant":
				changed.TenantID = "B"
			case "system":
				changed.System = true
			}
			derived, err := auth.WithPrincipal(ctx, changed)
			require.NoError(t, err)
			_, ok := scoped.DataScopeFromContext(derived)
			require.False(t, ok)
			derived, err = auth.WithPrincipal(derived, identity)
			require.NoError(t, err)
			_, ok = scoped.DataScopeFromContext(derived)
			require.False(t, ok)
			_, ok = scoped.DataScopeFromContext(ctx)
			require.True(t, ok)
		})
	}
}

func TestDataScopeSnapshotsAndTenantValidation(t *testing.T) {
	ctx, err := contextx.WithTenantID(context.Background(), "A")
	require.NoError(t, err)
	scope := scoped.Filtered(7, 8)
	scope.TenantID = "A"
	bound, err := scoped.WithDataScope(ctx, scope)
	require.NoError(t, err)
	scope.ScopeIDs[0] = 99
	got, ok := scoped.DataScopeFromContext(bound)
	require.True(t, ok)
	require.Equal(t, []int64{7, 8}, got.ScopeIDs)
	got.ScopeIDs[0] = 99
	again, ok := scoped.DataScopeFromContext(bound)
	require.True(t, ok)
	require.Equal(t, []int64{7, 8}, again.ScopeIDs)
	changed, err := contextx.WithTenantID(bound, "B")
	require.NoError(t, err)
	changed, err = contextx.WithTenantID(changed, "A")
	require.NoError(t, err)
	_, ok = scoped.DataScopeFromContext(changed)
	require.False(t, ok)
	scope.TenantID = "B"
	_, err = scoped.WithDataScope(ctx, scope)
	require.True(t, errors.Is(err, errors.Forbidden))
	_, err = scoped.ResolveDataScope(ctx, scoped.DataScopeResolverFunc(func(context.Context) (scoped.DataScope, error) { return scope, nil }))
	require.True(t, errors.Is(err, errors.Forbidden))
	withPrincipal, err := auth.WithPrincipal(bound, auth.Principal{SubjectID: "alice", TenantID: "A", ScopeID: 7})
	require.NoError(t, err)
	_, ok = scoped.DataScopeFromContext(withPrincipal)
	require.False(t, ok)
}
