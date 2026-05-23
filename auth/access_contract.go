package auth

import (
	"context"
	"strings"

	"gochen/domain/access"
)

// AccessDataScopeResolver 基于 auth 运行时求值 domain/access 数据边界。
type AccessDataScopeResolver struct{}

// ResolveDataScope 从当前 principal 与 auth 运行时上下文推导 domain/access 数据边界。
func (AccessDataScopeResolver) ResolveDataScope(ctx context.Context) (access.DataScope, error) {
	scope, err := ResolveDataScope(ctx, PrincipalDataScopeResolver{})
	if err != nil {
		return access.DataScope{}, err
	}
	return AccessDataScopeFromAuth(scope), nil
}

// WriteConstraintFromDecision 将 allow 决策投影为 domain/access 写约束。
func WriteConstraintFromDecision(decision AuthzDecision) access.WriteConstraint {
	return WriteConstraintFromWriteGuard(decision.WriteGuard())
}

// WriteConstraintFromWriteGuard 将 WriteGuard 转换为 domain/access 写约束。
func WriteConstraintFromWriteGuard(guard WriteGuard) access.WriteConstraint {
	guard = normalizeWriteGuard(guard)
	constraint := access.WriteConstraint{
		Metadata: access.ConstraintMetadata{
			DecisionID:      strings.TrimSpace(guard.DecisionID),
			SnapshotVersion: strings.TrimSpace(guard.SnapshotVersion),
			Consistency:     strings.TrimSpace(string(guard.Consistency)),
		},
	}
	if len(guard.Resources) == 0 {
		return constraint
	}
	constraint.Resources = make([]access.ResourceConstraint, 0, len(guard.Resources))
	for _, resource := range guard.Resources {
		constraint.Resources = append(constraint.Resources, access.ResourceConstraint{
			Kind:           resource.Kind,
			ResourceID:     resource.ResourceID,
			ManagedScopeID: resource.ManagedScopeID,
			GlobalScope:    resource.GlobalScope,
			TenantID:       resource.TenantID,
			Revision:       resource.Revision,
		})
	}
	return constraint
}

// ConstraintMetadataFromDecision 将授权决策转换为 domain/access 约束元数据。
func ConstraintMetadataFromDecision(decision AuthzDecision) access.ConstraintMetadata {
	guard := decision.WriteGuard()
	return access.ConstraintMetadata{
		DecisionID:      strings.TrimSpace(guard.DecisionID),
		SnapshotVersion: strings.TrimSpace(guard.SnapshotVersion),
		Consistency:     strings.TrimSpace(string(guard.Consistency)),
	}
}

// BindConstraintMetadata 将决策元数据写入 context，供下游审计/观测复用。
func BindConstraintMetadata(ctx context.Context, decision AuthzDecision) context.Context {
	return access.WithConstraintMetadata(ctx, ConstraintMetadataFromDecision(decision))
}

// ResourceBoundaryFromAuth 将授权资源转换为 domain/access 资源边界。
func ResourceBoundaryFromAuth(resource Resource) access.ResourceBoundary {
	resource = normalizeResource(resource)
	return access.ResourceBoundary{
		Kind:           resource.Kind,
		ID:             resource.ID,
		ManagedScopeID: resource.ManagedScopeID,
		GlobalScope:    resource.GlobalScope,
		TenantID:       resource.TenantID,
		OwnerID:        resource.OwnerID,
		Revision:       resource.Revision,
	}
}

// AuthResourceFromBoundary 将 domain/access 资源边界转换为授权资源。
func AuthResourceFromBoundary(boundary access.ResourceBoundary) Resource {
	return normalizeResource(Resource{
		Kind:           boundary.Kind,
		ID:             boundary.ID,
		ManagedScopeID: boundary.ManagedScopeID,
		GlobalScope:    boundary.GlobalScope,
		TenantID:       boundary.TenantID,
		OwnerID:        boundary.OwnerID,
		Revision:       boundary.Revision,
	})
}

// AccessDataScopeFromAuth 将 auth DataScope 转换为 domain/access DataScope。
func AccessDataScopeFromAuth(scope DataScope) access.DataScope {
	scope = normalizeDataScope(scope)
	return access.DataScope{
		ActiveScopeID:   scope.ActiveScopeID,
		VisibleScopeIDs: append([]int64(nil), scope.VisibleScopeIDs...),
		TenantIDs:       append([]int64(nil), scope.TenantIDs...),
		Mode:            accessScopeModeFromAuth(scope.Mode),
	}
}

// AuthDataScopeFromAccess 将 domain/access DataScope 转换为 auth DataScope。
func AuthDataScopeFromAccess(scope access.DataScope) DataScope {
	return normalizeDataScope(DataScope{
		ActiveScopeID:   scope.ActiveScopeID,
		VisibleScopeIDs: append([]int64(nil), scope.VisibleScopeIDs...),
		TenantIDs:       append([]int64(nil), scope.TenantIDs...),
		Mode:            authScopeModeFromAccess(scope.Mode),
	})
}

func bindAccessDataScope(ctx context.Context, scope DataScope, derived bool) context.Context {
	var accessScope access.DataScope
	if !dataScopeIsEmpty(scope) {
		accessScope = AccessDataScopeFromAuth(scope)
	}
	if derived {
		return access.WithDerivedDataScope(ctx, accessScope)
	}
	return access.WithDataScope(ctx, accessScope)
}

func accessScopeModeFromAuth(mode ScopeMode) access.ScopeMode {
	switch mode {
	case ScopeModeGlobal:
		return access.ScopeModeGlobal
	default:
		return access.ScopeModeScoped
	}
}

func authScopeModeFromAccess(mode access.ScopeMode) ScopeMode {
	switch mode {
	case access.ScopeModeGlobal:
		return ScopeModeGlobal
	default:
		return ScopeModeScoped
	}
}

type accessWriteAuditRecorder struct{}

// RecordWriteAudit 把 domain/access 写审计事件转发到 auth 运行时观测通道。
func (accessWriteAuditRecorder) RecordWriteAudit(ctx context.Context, record access.WriteAuditRecord) {
	RecordAccessWriteConstraintAudit(ctx, record.Operation, record.Constraint, record.Resources...)
}

func withDefaultAccessWriteAuditRecorder(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	if _, ok := access.WriteAuditRecorderFromContext(ctx); ok {
		return ctx
	}
	return access.WithWriteAuditRecorder(ctx, accessWriteAuditRecorder{})
}

// RecordAccessWriteConstraintAudit 记录一次 domain/access 写约束事件到 auth 观测通道。
func RecordAccessWriteConstraintAudit(ctx context.Context, operation string, constraint access.WriteConstraint, resources ...access.ResourceConstraint) {
	metadata := constraintMetadataForAccessAudit(ctx, constraint)
	guard := WriteGuard{
		DecisionID:      metadata.DecisionID,
		SnapshotVersion: metadata.SnapshotVersion,
		Consistency:     ConsistencyMode(metadata.Consistency),
	}
	for _, resource := range constraint.Resources {
		guard.Resources = append(guard.Resources, accessResourceConstraintToWriteGuard(resource))
	}
	logResources := make([]ResourceWriteGuard, 0, len(resources))
	for _, resource := range resources {
		logResources = append(logResources, accessResourceConstraintToWriteGuard(resource))
	}
	RecordWriteAudit(ctx, operation, guard, logResources...)
}

func accessResourceConstraintToWriteGuard(resource access.ResourceConstraint) ResourceWriteGuard {
	return normalizeResourceWriteGuard(ResourceWriteGuard{
		Kind:           resource.Kind,
		ResourceID:     resource.ResourceID,
		ManagedScopeID: resource.ManagedScopeID,
		GlobalScope:    resource.GlobalScope,
		TenantID:       resource.TenantID,
		Revision:       resource.Revision,
	})
}

func constraintMetadataForAccessAudit(ctx context.Context, constraint access.WriteConstraint) access.ConstraintMetadata {
	metadata := access.ConstraintMetadata{
		DecisionID:      strings.TrimSpace(constraint.Metadata.DecisionID),
		SnapshotVersion: strings.TrimSpace(constraint.Metadata.SnapshotVersion),
		Consistency:     strings.TrimSpace(constraint.Metadata.Consistency),
	}
	ctxMetadata, ok := access.ConstraintMetadataFromContext(ctx)
	if !ok {
		return metadata
	}
	if metadata.DecisionID == "" {
		metadata.DecisionID = ctxMetadata.DecisionID
	}
	if metadata.SnapshotVersion == "" {
		metadata.SnapshotVersion = ctxMetadata.SnapshotVersion
	}
	if metadata.Consistency == "" {
		metadata.Consistency = ctxMetadata.Consistency
	}
	return metadata
}
