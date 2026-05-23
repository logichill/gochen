package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/domain/access"
	"gochen/errors"
)

type testResource struct {
	ID string
}

func TestWithPrincipal_NormalizesAndWritesContext(t *testing.T) {
	ctx, err := WithPrincipal(context.Background(), Principal{
		SubjectID:     42,
		HomeTenantID:  9,
		HomeScopeID:   11,
		ActiveScopeID: 21,
		Permissions:   []string{" user:read ", "user:read", "user:write"},
	})
	require.NoError(t, err)

	principal, ok := PrincipalFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, int64(42), principal.SubjectID)
	require.Equal(t, int64(9), principal.HomeTenantID)
	require.Equal(t, int64(11), principal.HomeScopeID)
	require.Equal(t, int64(21), principal.ActiveScopeID)
	require.ElementsMatch(t, []string{"user:read", "user:write"}, principal.Permissions)
	require.True(t, principal.HasPermission("user:read"))
	require.True(t, principal.AllowsPermission("user:read"))
	require.Equal(t, int64(42), UserID(ctx))

	scope, ok := DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, ScopeModeScoped, scope.Mode)
	require.Equal(t, int64(21), scope.ActiveScopeID)
	require.Equal(t, []int64{21}, scope.VisibleScopeIDs)
	require.True(t, dataScopeIsDerivedFromContext(ctx))

	accessScope, ok := access.DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, access.ScopeModeScoped, accessScope.Mode)
	require.Equal(t, int64(21), accessScope.ActiveScopeID)
	require.Equal(t, []int64{21}, accessScope.VisibleScopeIDs)
	require.True(t, access.DataScopeIsDerivedFromContext(ctx))
}

func TestWithPrincipal_RebindsDerivedDataScope(t *testing.T) {
	ctx, err := WithPrincipal(context.Background(), Principal{SubjectID: 1, HomeTenantID: 10, ActiveScopeID: 101})
	require.NoError(t, err)

	ctx, err = WithPrincipal(ctx, Principal{SubjectID: 2, HomeTenantID: 20, ActiveScopeID: 202})
	require.NoError(t, err)

	scope, ok := DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, int64(202), scope.ActiveScopeID)
	require.Equal(t, []int64{202}, scope.VisibleScopeIDs)
	require.True(t, dataScopeIsDerivedFromContext(ctx))

	accessScope, ok := access.DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, access.ScopeModeScoped, accessScope.Mode)
	require.Equal(t, int64(202), accessScope.ActiveScopeID)
	require.Equal(t, []int64{202}, accessScope.VisibleScopeIDs)
	require.True(t, access.DataScopeIsDerivedFromContext(ctx))
}

func TestWithPrincipal_ProjectsGlobalDataScope(t *testing.T) {
	ctx, err := WithPrincipal(context.Background(), Principal{SubjectID: 1, IsSystem: true})
	require.NoError(t, err)

	scope, ok := DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, ScopeModeGlobal, scope.Mode)
	require.Zero(t, scope.ActiveScopeID)
	require.Empty(t, scope.VisibleScopeIDs)
	require.True(t, dataScopeIsDerivedFromContext(ctx))

	accessScope, ok := access.DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, access.ScopeModeGlobal, accessScope.Mode)
	require.Zero(t, accessScope.ActiveScopeID)
	require.Empty(t, accessScope.VisibleScopeIDs)
	require.True(t, access.DataScopeIsDerivedFromContext(ctx))
}

func TestWithPrincipal_DoesNotProjectMissingActiveScopeAsGlobal(t *testing.T) {
	ctx, err := WithPrincipal(context.Background(), Principal{SubjectID: 1})
	require.NoError(t, err)

	_, ok := DataScopeFromContext(ctx)
	require.False(t, ok)

	_, ok = access.DataScopeFromContext(ctx)
	require.False(t, ok)
}

func TestWithPrincipal_ClearsDerivedDataScopeWhenPrincipalHasNoScope(t *testing.T) {
	ctx, err := WithPrincipal(context.Background(), Principal{SubjectID: 1, ActiveScopeID: 101})
	require.NoError(t, err)

	ctx, err = WithPrincipal(ctx, Principal{SubjectID: 2})
	require.NoError(t, err)

	_, ok := DataScopeFromContext(ctx)
	require.False(t, ok)
	require.False(t, dataScopeIsDerivedFromContext(ctx))

	_, ok = access.DataScopeFromContext(ctx)
	require.False(t, ok)
	require.False(t, access.DataScopeIsDerivedFromContext(ctx))
}

func TestDerivedDataScope_WithInvalidIDsDoesNotBecomeGlobal(t *testing.T) {
	ctx := withDerivedDataScope(context.Background(), DataScope{
		ActiveScopeID:   -1,
		VisibleScopeIDs: []int64{0, -2},
		TenantIDs:       []int64{-3},
	})

	_, ok := DataScopeFromContext(ctx)
	require.False(t, ok)
	require.False(t, dataScopeIsDerivedFromContext(ctx))

	_, ok = access.DataScopeFromContext(ctx)
	require.False(t, ok)
	require.False(t, access.DataScopeIsDerivedFromContext(ctx))
}

func TestWithPrincipal_PreservesExplicitDataScope(t *testing.T) {
	ctx, err := WithDataScope(context.Background(), DataScope{
		ActiveScopeID:   501,
		VisibleScopeIDs: []int64{501, 502},
		Mode:            ScopeModeScoped,
	})
	require.NoError(t, err)

	ctx, err = WithPrincipal(ctx, Principal{SubjectID: 7, HomeTenantID: 70, ActiveScopeID: 707})
	require.NoError(t, err)

	scope, ok := DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, int64(501), scope.ActiveScopeID)
	require.Equal(t, []int64{501, 502}, scope.VisibleScopeIDs)
	require.False(t, dataScopeIsDerivedFromContext(ctx))

	accessScope, ok := access.DataScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, access.ScopeModeScoped, accessScope.Mode)
	require.Equal(t, int64(501), accessScope.ActiveScopeID)
	require.Equal(t, []int64{501, 502}, accessScope.VisibleScopeIDs)
	require.False(t, access.DataScopeIsDerivedFromContext(ctx))
}

func TestWithPrincipal_LeavesExplicitTenantContextUntouched(t *testing.T) {
	ctx, err := WithTenantID(context.Background(), "tenant-old")
	require.NoError(t, err)

	ctx, err = WithPrincipal(ctx, Principal{SubjectID: 8, ActiveScopeID: 808})
	require.NoError(t, err)

	require.Equal(t, "tenant-old", TenantID(ctx))
}

func TestPermissionPatternMatches(t *testing.T) {
	require.True(t, PermissionPatternMatches("api:user:*", "api:user:read"))
	require.True(t, PermissionPatternMatches("*:*:*", "api:user:read"))
	require.True(t, PermissionPatternMatches("API:USER:*", "api:user:read"))
	require.False(t, PermissionPatternMatches("api:user", "api:user:read"))
	require.False(t, PermissionPatternMatches("api:group:*", "api:user:read"))
}

func TestPrincipalAllowsPermission_GlobalWildcardForNonSystemPrincipal(t *testing.T) {
	principal := Principal{
		SubjectID:     1,
		ActiveScopeID: 1,
		Permissions:   []string{"*:*:*"},
	}

	require.True(t, principal.AllowsPermission("api:user:read"))
	require.True(t, principal.AllowsPermission("api:tenant:write"))
	require.True(t, principal.AllowsPermission("menu:iam:view"))
}

func TestPrincipalDataScopeResolver_DefaultsToActiveScope(t *testing.T) {
	resolver := PrincipalDataScopeResolver{}

	scope, err := resolver.Resolve(context.Background(), AuthzEvalContext{Principal: Principal{SubjectID: 7, ActiveScopeID: 88}})
	require.NoError(t, err)
	require.Equal(t, ScopeModeScoped, scope.Mode)
	require.Equal(t, int64(88), scope.ActiveScopeID)
	require.Equal(t, []int64{88}, scope.VisibleScopeIDs)

	global, err := resolver.Resolve(context.Background(), AuthzEvalContext{Principal: Principal{SubjectID: 1, IsSystem: true}})
	require.NoError(t, err)
	require.Equal(t, ScopeModeGlobal, global.Mode)

	_, err = resolver.Resolve(context.Background(), AuthzEvalContext{Principal: Principal{SubjectID: 9}})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestAuthorizer_FillsManagedScopeFromBoundDataScope(t *testing.T) {
	resolver := TypedResourceResolver(func(target testResource) (Resource, bool) {
		return Resource{Kind: "document", ID: target.ID}, true
	})
	authorizer, err := NewAuthorizer(EvaluatorFunc(func(ctx context.Context, principal Principal, permission string, resources []Resource) (AuthzDecision, error) {
		require.Equal(t, int64(99), principal.SubjectID)
		require.Equal(t, "doc:read", permission)
		require.Len(t, resources, 1)
		require.Equal(t, int64(501), resources[0].ManagedScopeID)
		return AllowDecision(resources...), nil
	}), resolver)
	require.NoError(t, err)

	ctx, err := WithPrincipal(context.Background(), Principal{SubjectID: 99, ActiveScopeID: 500})
	require.NoError(t, err)
	ctx, err = WithDataScope(ctx, DataScope{ActiveScopeID: 500, VisibleScopeIDs: []int64{501}, Mode: ScopeModeScoped})
	require.NoError(t, err)

	decision, err := authorizer.Authorize(ctx, "doc:read", testResource{ID: "doc-1"})
	require.NoError(t, err)
	require.Equal(t, EffectAllow, decision.Effect)
	require.Len(t, decision.AuthorizedResources, 1)
	require.Equal(t, int64(501), decision.AuthorizedResources[0].ManagedScopeID)
}

func TestAuthorizer_MissingPrincipalReturnsDenyPreservingEvalMetadata(t *testing.T) {
	authorizer, err := NewAuthorizer(EvaluatorFunc(func(ctx context.Context, principal Principal, permission string, resources []Resource) (AuthzDecision, error) {
		return AllowDecision(resources...), nil
	}), nil)
	require.NoError(t, err)

	ctx, err := WithConsistencyMode(context.Background(), ConsistencyModeBoundedStaleness)
	require.NoError(t, err)
	ctx, err = WithExecutionMetadata(ctx, ExecutionMetadata{RequestID: "req-11"})
	require.NoError(t, err)
	ctx, err = WithSnapshotResolver(ctx, SnapshotResolverFunc(func(ctx context.Context, eval AuthzEvalContext) (SnapshotResolution, error) {
		require.True(t, isZeroPrincipal(eval.Principal))
		require.Equal(t, "req-11", eval.Execution.RequestID)
		return SnapshotResolution{Version: "policy:req-11"}, nil
	}))
	require.NoError(t, err)

	decision, err := authorizer.Authorize(ctx, "system:read")
	require.NoError(t, err)
	require.Equal(t, EffectDeny, decision.Effect)
	require.Equal(t, ReasonCodePrincipalMissing, decision.ReasonCode)
	require.Equal(t, "policy:req-11", decision.SnapshotVersion)
	require.Equal(t, ConsistencyModeBoundedStaleness, decision.Consistency)
}

func TestBindAuthzEvalContext_PinsSnapshotVersion(t *testing.T) {
	calls := 0
	ctx, err := WithPrincipal(context.Background(), Principal{SubjectID: 77, ActiveScopeID: 12})
	require.NoError(t, err)
	ctx, err = WithSnapshotResolver(ctx, SnapshotResolverFunc(func(ctx context.Context, eval AuthzEvalContext) (SnapshotResolution, error) {
		calls++
		if calls == 1 {
			return SnapshotResolution{Version: "snap-1"}, nil
		}
		return SnapshotResolution{Version: "snap-2"}, nil
	}))
	require.NoError(t, err)

	ctx, eval, err := BindAuthzEvalContext(ctx)
	require.NoError(t, err)
	require.Equal(t, "snap-1", eval.SnapshotVersion)

	rebound, err := EvalContextFromContext(ctx)
	require.NoError(t, err)
	require.Equal(t, "snap-1", rebound.SnapshotVersion)
	require.Equal(t, 1, calls)
}

func TestDecisionWriteConstraint_UsesManagedScopeAndRevision(t *testing.T) {
	decision := AllowDecision(Resource{Kind: "document", ID: "7", ManagedScopeID: 301, Revision: "9"})
	decision.ID = "dec-1"
	decision.SnapshotVersion = "snap-1"
	decision.Consistency = ConsistencyModeStrong

	guard := decision.WriteGuard()
	require.Len(t, guard.Resources, 1)
	require.Equal(t, int64(301), guard.Resources[0].ManagedScopeID)
	require.Equal(t, "9", guard.Resources[0].Revision)

	split, err := guard.SplitByTargets([]Resource{{Kind: "document", ID: "7", ManagedScopeID: 301, Revision: "9"}})
	require.NoError(t, err)
	require.Len(t, split, 1)
	require.Equal(t, int64(301), split[0].Resources[0].ManagedScopeID)
}

func TestWriteConstraintFromWriteGuard_NormalizesManualGuard(t *testing.T) {
	constraint := WriteConstraintFromWriteGuard(WriteGuard{
		DecisionID:      " dec-1 ",
		SnapshotVersion: " snap-1 ",
		Resources: []ResourceWriteGuard{{
			Kind:           " document ",
			ResourceID:     " 7 ",
			ManagedScopeID: 301,
			GlobalScope:    true,
			TenantID:       " tenant-1 ",
			Revision:       " 9 ",
		}},
	})

	require.Equal(t, "dec-1", constraint.Metadata.DecisionID)
	require.Equal(t, "snap-1", constraint.Metadata.SnapshotVersion)
	require.Equal(t, string(ConsistencyModeStrong), constraint.Metadata.Consistency)
	require.Len(t, constraint.Resources, 1)
	require.Equal(t, "document", constraint.Resources[0].Kind)
	require.Equal(t, "7", constraint.Resources[0].ResourceID)
	// GlobalScope=true 时 ManagedScopeID 必须被清零，避免两种 scope 表示混用。
	require.Equal(t, int64(0), constraint.Resources[0].ManagedScopeID)
	require.True(t, constraint.Resources[0].GlobalScope)
	require.Equal(t, "tenant-1", constraint.Resources[0].TenantID)
	require.Equal(t, "9", constraint.Resources[0].Revision)
}

func TestWriteConstraintFromWriteGuard_PreservesManagedScopeIDWhenNotGlobal(t *testing.T) {
	constraint := WriteConstraintFromWriteGuard(WriteGuard{
		Resources: []ResourceWriteGuard{{
			Kind:           "document",
			ResourceID:     "42",
			ManagedScopeID: 501,
			GlobalScope:    false,
		}},
	})

	require.Len(t, constraint.Resources, 1)
	require.Equal(t, int64(501), constraint.Resources[0].ManagedScopeID)
	require.False(t, constraint.Resources[0].GlobalScope)
}

func TestAccessDataScopeFromAuth_RoundTrip(t *testing.T) {
	t.Run("scoped round-trip", func(t *testing.T) {
		authScope := DataScope{
			ActiveScopeID:   10,
			VisibleScopeIDs: []int64{10, 20},
			TenantIDs:       []int64{5},
			Mode:            ScopeModeScoped,
		}
		accessScope := AccessDataScopeFromAuth(authScope)
		require.Equal(t, access.ScopeModeScoped, accessScope.Mode)
		require.Equal(t, int64(10), accessScope.ActiveScopeID)
		require.Equal(t, []int64{10, 20}, accessScope.VisibleScopeIDs)
		require.Equal(t, []int64{5}, accessScope.TenantIDs)

		back := AuthDataScopeFromAccess(accessScope)
		require.Equal(t, ScopeModeScoped, back.Mode)
		require.Equal(t, int64(10), back.ActiveScopeID)
		require.Equal(t, []int64{10, 20}, back.VisibleScopeIDs)
	})

	t.Run("global round-trip", func(t *testing.T) {
		authScope := DataScope{Mode: ScopeModeGlobal}
		accessScope := AccessDataScopeFromAuth(authScope)
		require.Equal(t, access.ScopeModeGlobal, accessScope.Mode)
		require.Zero(t, accessScope.ActiveScopeID)
		require.Empty(t, accessScope.VisibleScopeIDs)

		back := AuthDataScopeFromAccess(accessScope)
		require.Equal(t, ScopeModeGlobal, back.Mode)
	})
}

func TestWithDataScope_EmptyScopeNotReadable(t *testing.T) {
	// 显式绑定空 DataScope 不应被读出为 Global scope——
	// 空 scope 被 normalizeDataScope 设为 ScopeModeGlobal 后，hasDataScope 本会误报 true；
	// 修复后 dataScopeIsEmpty 保护在 DataScopeFromContext 中统一生效。
	ctx, err := WithDataScope(context.Background(), DataScope{})
	require.NoError(t, err)

	_, ok := DataScopeFromContext(ctx)
	require.False(t, ok)

	_, ok = access.DataScopeFromContext(ctx)
	require.False(t, ok)
}

func TestConstraintMetadataFromDecision_NormalizesFields(t *testing.T) {
	decision := AllowDecision()
	decision.ID = " dec-99 "
	decision.SnapshotVersion = " snap-99 "
	decision.Consistency = ConsistencyModeBoundedStaleness

	metadata := ConstraintMetadataFromDecision(decision)
	require.Equal(t, "dec-99", metadata.DecisionID)
	require.Equal(t, "snap-99", metadata.SnapshotVersion)
	require.Equal(t, string(ConsistencyModeBoundedStaleness), metadata.Consistency)
}

func TestBindConstraintMetadata_WritesToContext(t *testing.T) {
	decision := AllowDecision()
	decision.ID = "dec-bind"
	decision.SnapshotVersion = "snap-bind"
	decision.Consistency = ConsistencyModeStrong

	ctx := BindConstraintMetadata(context.Background(), decision)
	metadata, ok := access.ConstraintMetadataFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "dec-bind", metadata.DecisionID)
	require.Equal(t, "snap-bind", metadata.SnapshotVersion)
	require.Equal(t, string(ConsistencyModeStrong), metadata.Consistency)
}
