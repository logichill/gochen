package auth

import (
	"context"
	"fmt"
	"strings"

	"gochen/errors"
)

// ResourceWriteGuard 表达单个资源的显式写入约束。
//
// GlobalScope=true 表示资源不受 managed scope 约束，此时 ManagedScopeID 必须为 0。
// 比较时"目标声明 GlobalScope" 与 "目标指定 ManagedScopeID" 是互斥的判定路径。
type ResourceWriteGuard struct {
	Kind           string
	ResourceID     string
	ManagedScopeID int64
	GlobalScope    bool
	TenantID       string
	Revision       string
}

// WriteGuard 表达一次授权允许后的显式写入边界。
type WriteGuard struct {
	Resources       []ResourceWriteGuard
	DecisionID      string
	SnapshotVersion string
	Consistency     ConsistencyMode
}

// WriteGuard 将 allow 决策投影为显式写入约束。
func (d AuthzDecision) WriteGuard() WriteGuard {
	decision := normalizeDecision(d)
	guard := WriteGuard{DecisionID: decision.ID, SnapshotVersion: decision.SnapshotVersion, Consistency: decision.Consistency}
	if decision.Effect != EffectAllow || len(decision.AuthorizedResources) == 0 {
		return guard
	}
	guard.Resources = make([]ResourceWriteGuard, 0, len(decision.AuthorizedResources))
	for _, resource := range decision.AuthorizedResources {
		guard.Resources = append(guard.Resources, resource.writeGuard())
	}
	return normalizeWriteGuard(guard)
}

// RequireResource 要求 guard 中存在且仅存在一个匹配资源。
func (g WriteGuard) RequireResource(kind, resourceID string) (ResourceWriteGuard, error) {
	guard := normalizeWriteGuard(g)
	if len(guard.Resources) == 0 {
		return ResourceWriteGuard{}, errors.NewCode(errors.Forbidden, "write guard does not authorize any resource")
	}
	kind = strings.TrimSpace(kind)
	resourceID = strings.TrimSpace(resourceID)
	var matched ResourceWriteGuard
	count := 0
	for _, resource := range guard.Resources {
		if resource.Kind != kind {
			continue
		}
		if resourceID != "" && resource.ResourceID != "" && resource.ResourceID != resourceID {
			continue
		}
		if resourceID != "" && resource.ResourceID == "" {
			matched = resource
			count++
			continue
		}
		if resourceID == "" && resource.ResourceID != "" {
			continue
		}
		matched = resource
		count++
	}
	switch {
	case count == 1:
		return matched, nil
	case count == 0:
		return ResourceWriteGuard{}, errors.NewCode(errors.Forbidden, "write guard does not authorize the target resource").
			WithContext("resource_kind", kind).
			WithContext("resource_id", resourceID)
	default:
		return ResourceWriteGuard{}, errors.NewCode(errors.InvalidInput, "write guard matches multiple target resources").
			WithContext("resource_kind", kind).
			WithContext("resource_id", resourceID)
	}
}

// SplitByTargets 按目标资源 hint 将批量 WriteGuard 安全拆分为多个单资源 guard。
func (g WriteGuard) SplitByTargets(targets []Resource) ([]WriteGuard, error) {
	guard := normalizeWriteGuard(g)
	if len(targets) == 0 {
		return nil, nil
	}
	if len(guard.Resources) != len(targets) {
		return nil, errors.NewCode(errors.InvalidInput, "write guard resource count mismatch").
			WithContext("expected", len(targets)).
			WithContext("actual", len(guard.Resources))
	}
	normalizedTargets := make([]Resource, 0, len(targets))
	for _, target := range targets {
		normalizedTargets = append(normalizedTargets, normalizeResource(target))
	}
	used := make([]bool, len(guard.Resources))
	result := make([]WriteGuard, 0, len(normalizedTargets))
	for _, target := range normalizedTargets {
		index, err := matchWriteGuardResourceIndex(guard.Resources, used, target)
		if err != nil {
			return nil, err
		}
		used[index] = true
		result = append(result, WriteGuard{Resources: []ResourceWriteGuard{guard.Resources[index]}, DecisionID: guard.DecisionID, SnapshotVersion: guard.SnapshotVersion, Consistency: guard.Consistency})
	}
	return result, nil
}

// ScopedContext 将单资源 WriteGuard 的 managed scope 绑定到 ctx。
func (g WriteGuard) ScopedContext(ctx context.Context) (context.Context, error) {
	guard := normalizeWriteGuard(g)
	if len(guard.Resources) != 1 {
		return nil, errors.NewCode(errors.InvalidInput, "write guard must contain exactly one resource to derive scoped context").WithContext("resource_count", len(guard.Resources))
	}
	return WithResourceWriteGuardScope(ctx, guard.Resources[0])
}

func (r Resource) writeGuard() ResourceWriteGuard {
	resource := normalizeResource(r)
	return normalizeResourceWriteGuard(ResourceWriteGuard{
		Kind:           resource.Kind,
		ResourceID:     resource.ID,
		ManagedScopeID: resource.ManagedScopeID,
		GlobalScope:    resource.GlobalScope,
		TenantID:       resource.TenantID,
		Revision:       resource.Revision,
	})
}

func normalizeWriteGuard(guard WriteGuard) WriteGuard {
	guard.DecisionID = strings.TrimSpace(guard.DecisionID)
	guard.SnapshotVersion = strings.TrimSpace(guard.SnapshotVersion)
	guard.Consistency = normalizeConsistencyMode(guard.Consistency)
	if guard.Consistency == ConsistencyModeUnspecified {
		guard.Consistency = ConsistencyModeStrong
	}
	if len(guard.Resources) == 0 {
		guard.Resources = nil
		return guard
	}
	resources := make([]ResourceWriteGuard, 0, len(guard.Resources))
	for _, resource := range guard.Resources {
		resources = append(resources, normalizeResourceWriteGuard(resource))
	}
	guard.Resources = resources
	return guard
}

func normalizeResourceWriteGuard(guard ResourceWriteGuard) ResourceWriteGuard {
	guard.Kind = strings.TrimSpace(guard.Kind)
	guard.ResourceID = strings.TrimSpace(guard.ResourceID)
	guard.ManagedScopeID = NormalizePositiveID(guard.ManagedScopeID)
	guard.TenantID = strings.TrimSpace(guard.TenantID)
	guard.Revision = strings.TrimSpace(guard.Revision)
	if guard.GlobalScope {
		guard.ManagedScopeID = 0
	}
	return guard
}

// WithResourceWriteGuardScope 将单资源写入约束中的 managed scope 绑定到 ctx。
//
// Global 资源（GlobalScope=true）没有 managed scope 边界，原样返回 ctx。
func WithResourceWriteGuardScope(ctx context.Context, guard ResourceWriteGuard) (context.Context, error) {
	ctx, err := RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	guard = normalizeResourceWriteGuard(guard)
	if guard.GlobalScope || guard.ManagedScopeID == 0 {
		return ctx, nil
	}
	return WithDataScope(ctx, DataScope{ActiveScopeID: guard.ManagedScopeID, VisibleScopeIDs: []int64{guard.ManagedScopeID}, Mode: ScopeModeScoped})
}

func matchWriteGuardResourceIndex(resources []ResourceWriteGuard, used []bool, target Resource) (int, error) {
	matched := make([]int, 0, 1)
	for i, resource := range resources {
		if used[i] {
			continue
		}
		if !writeGuardMatchesTarget(resource, target) {
			continue
		}
		matched = append(matched, i)
	}
	switch len(matched) {
	case 0:
		return -1, errors.NewCode(errors.Forbidden, "write guard does not authorize the target resource").WithContext("target", formatWriteGuardTarget(target))
	case 1:
		return matched[0], nil
	default:
		if writeGuardResourcesEquivalent(resources, matched) {
			return matched[0], nil
		}
		return -1, errors.NewCode(errors.InvalidInput, "write guard matches multiple target resources").WithContext("target", formatWriteGuardTarget(target))
	}
}

func writeGuardMatchesTarget(resource ResourceWriteGuard, target Resource) bool {
	if target.Kind != "" && resource.Kind != target.Kind {
		return false
	}
	if target.ID != "" && resource.ResourceID != target.ID {
		return false
	}
	// Global vs managed 是互斥判定：若 target 声明 GlobalScope 但 guard 不是 global 则拒绝；
	// 反之若 guard 是 global，则不再按 ManagedScopeID 比较。
	if target.GlobalScope && !resource.GlobalScope {
		return false
	}
	if !target.GlobalScope && !resource.GlobalScope && target.ManagedScopeID != 0 && resource.ManagedScopeID != target.ManagedScopeID {
		return false
	}
	// 一旦 target 显式声明 TenantID，guard 必须精确匹配（包括空值——空 TenantID
	// 不再视为 "匹配所有 tenant" 以免越权）。
	if target.TenantID != "" && resource.TenantID != target.TenantID {
		return false
	}
	if target.Revision != "" && resource.Revision != target.Revision {
		return false
	}
	return true
}

func writeGuardResourcesEquivalent(resources []ResourceWriteGuard, indexes []int) bool {
	if len(indexes) <= 1 {
		return true
	}
	first := normalizeResourceWriteGuard(resources[indexes[0]])
	for _, index := range indexes[1:] {
		if !resourceWriteGuardsEqual(normalizeResourceWriteGuard(resources[index]), first) {
			return false
		}
	}
	return true
}

func resourceWriteGuardsEqual(left, right ResourceWriteGuard) bool {
	return left.Kind == right.Kind &&
		left.ResourceID == right.ResourceID &&
		left.ManagedScopeID == right.ManagedScopeID &&
		left.GlobalScope == right.GlobalScope &&
		left.TenantID == right.TenantID &&
		left.Revision == right.Revision
}

func formatWriteGuardTarget(target Resource) string {
	target = normalizeResource(target)
	parts := make([]string, 0, 6)
	if target.Kind != "" {
		parts = append(parts, "kind="+target.Kind)
	}
	if target.ID != "" {
		parts = append(parts, "id="+target.ID)
	}
	if target.GlobalScope {
		parts = append(parts, "global_scope=true")
	} else if target.ManagedScopeID != 0 {
		parts = append(parts, fmt.Sprintf("managed_scope_id=%d", target.ManagedScopeID))
	}
	if target.TenantID != "" {
		parts = append(parts, "tenant_id="+target.TenantID)
	}
	if target.Revision != "" {
		parts = append(parts, "revision="+target.Revision)
	}
	if len(parts) == 0 {
		return "<empty>"
	}
	return strings.Join(parts, ",")
}
