package access

import (
	"context"
	"fmt"
	"strings"

	"gochen/domain"
	"gochen/errors"
)

// ResourceBoundary 表达一个资源在应用层可见的授权边界信息。
//
// GlobalScope=true 表示资源不受 managed scope 约束（如平台级资源）；
// TenantID 为结构化租户归属字段，授权判定不应依赖 OwnerID 字符串前缀。
type ResourceBoundary struct {
	Kind           string
	ID             string
	ManagedScopeID int64
	GlobalScope    bool
	TenantID       string
	OwnerID        string
	Revision       string
}

// ResourceConstraint 表达针对单个资源的写入约束。
type ResourceConstraint struct {
	Kind           string
	ResourceID     string
	ManagedScopeID int64
	GlobalScope    bool
	TenantID       string
	Revision       string
}

// WriteConstraint 表达一次写入允许后的显式边界。
//
// Metadata 承载与写入决策相伴的观测元数据（决策 ID、快照版本、一致性级别），
// 保证 auth.Authorize → repo 层的同一份元数据不会因跨层包装丢失。
type WriteConstraint struct {
	Resources []ResourceConstraint
	Metadata  ConstraintMetadata
}

// ScopedContext 将单资源约束的 managed scope 绑定到 ctx。
//
// 要求 WriteConstraint 恰好包含一个资源；若资源声明 GlobalScope 或
// ManagedScopeID 为 0，则原样返回 ctx。
func (c WriteConstraint) ScopedContext(ctx context.Context) (context.Context, error) {
	constraint := normalizeWriteConstraint(c)
	if len(constraint.Resources) != 1 {
		return nil, errors.NewCode(errors.InvalidInput, "write constraint must contain exactly one resource to derive scoped context").
			WithContext("resource_count", len(constraint.Resources))
	}
	resource := constraint.Resources[0]
	if resource.GlobalScope || resource.ManagedScopeID == 0 {
		return ctx, nil
	}
	return WithDataScope(ctx, DataScope{
		ActiveScopeID:   resource.ManagedScopeID,
		VisibleScopeIDs: []int64{resource.ManagedScopeID},
		Mode:            ScopeModeScoped,
	}), nil
}

// IWriteConstraintRepository 定义带显式写入约束的仓储能力。
type IWriteConstraintRepository[T domain.IEntity[ID], ID comparable] interface {
	CreateWithConstraint(ctx context.Context, e T, constraint WriteConstraint) error
	UpdateWithConstraint(ctx context.Context, e T, constraint WriteConstraint) error
	DeleteWithConstraint(ctx context.Context, id ID, constraint WriteConstraint) error
}

// IWriteConstraintBatchRepository 定义带显式写入约束的批量写能力。
type IWriteConstraintBatchRepository[T domain.IEntity[ID], ID comparable] interface {
	CreateAllWithConstraint(ctx context.Context, entities []T, constraint WriteConstraint) error
	UpdateAllWithConstraint(ctx context.Context, entities []T, constraint WriteConstraint) error
	DeleteAllWithConstraint(ctx context.Context, ids []ID, constraint WriteConstraint) error
}

// IResourceBoundaryRepository 定义“按资源 ID 解析边界”的读能力。
type IResourceBoundaryRepository[T domain.IEntity[ID], ID comparable] interface {
	ResolveResourceByID(ctx context.Context, id ID) (ResourceBoundary, error)
}

// RequireResource 要求约束中存在且仅存在一个匹配资源。
func (c WriteConstraint) RequireResource(kind, resourceID string) (ResourceConstraint, error) {
	constraint := normalizeWriteConstraint(c)
	if len(constraint.Resources) == 0 {
		return ResourceConstraint{}, errors.NewCode(errors.Forbidden, "write constraint does not authorize any resource")
	}
	kind = strings.TrimSpace(kind)
	resourceID = strings.TrimSpace(resourceID)
	var matched ResourceConstraint
	count := 0
	for _, resource := range constraint.Resources {
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
		return ResourceConstraint{}, errors.NewCode(errors.Forbidden, "write constraint does not authorize the target resource").
			WithContext("resource_kind", kind).
			WithContext("resource_id", resourceID)
	default:
		return ResourceConstraint{}, errors.NewCode(errors.InvalidInput, "write constraint matches multiple target resources").
			WithContext("resource_kind", kind).
			WithContext("resource_id", resourceID)
	}
}

// SplitByTargets 按目标资源 hint 将批量约束安全拆分为多个单资源约束。
func (c WriteConstraint) SplitByTargets(targets []ResourceBoundary) ([]WriteConstraint, error) {
	constraint := normalizeWriteConstraint(c)
	if len(targets) == 0 {
		return nil, nil
	}
	if len(constraint.Resources) != len(targets) {
		return nil, errors.NewCode(errors.InvalidInput, "write constraint resource count mismatch").
			WithContext("expected", len(targets)).
			WithContext("actual", len(constraint.Resources))
	}
	normalizedTargets := make([]ResourceBoundary, 0, len(targets))
	for _, target := range targets {
		normalizedTargets = append(normalizedTargets, normalizeResourceBoundary(target))
	}
	used := make([]bool, len(constraint.Resources))
	result := make([]WriteConstraint, 0, len(normalizedTargets))
	for _, target := range normalizedTargets {
		index, err := matchConstraintResourceIndex(constraint.Resources, used, target)
		if err != nil {
			return nil, err
		}
		used[index] = true
		result = append(result, WriteConstraint{
			Resources: []ResourceConstraint{constraint.Resources[index]},
			Metadata:  constraint.Metadata,
		})
	}
	return result, nil
}

func normalizeWriteConstraint(constraint WriteConstraint) WriteConstraint {
	if len(constraint.Resources) == 0 {
		constraint.Resources = nil
		return constraint
	}
	resources := make([]ResourceConstraint, 0, len(constraint.Resources))
	for _, resource := range constraint.Resources {
		resources = append(resources, normalizeResourceConstraint(resource))
	}
	constraint.Resources = resources
	return constraint
}

func normalizeResourceConstraint(resource ResourceConstraint) ResourceConstraint {
	resource.Kind = strings.TrimSpace(resource.Kind)
	resource.ResourceID = strings.TrimSpace(resource.ResourceID)
	resource.ManagedScopeID = NormalizePositiveID(resource.ManagedScopeID)
	resource.TenantID = strings.TrimSpace(resource.TenantID)
	resource.Revision = strings.TrimSpace(resource.Revision)
	if resource.GlobalScope {
		resource.ManagedScopeID = 0
	}
	return resource
}

func normalizeResourceBoundary(boundary ResourceBoundary) ResourceBoundary {
	boundary.Kind = strings.TrimSpace(boundary.Kind)
	boundary.ID = strings.TrimSpace(boundary.ID)
	boundary.ManagedScopeID = NormalizePositiveID(boundary.ManagedScopeID)
	boundary.TenantID = strings.TrimSpace(boundary.TenantID)
	boundary.OwnerID = strings.TrimSpace(boundary.OwnerID)
	boundary.Revision = strings.TrimSpace(boundary.Revision)
	if boundary.GlobalScope {
		boundary.ManagedScopeID = 0
	}
	return boundary
}

func matchConstraintResourceIndex(resources []ResourceConstraint, used []bool, target ResourceBoundary) (int, error) {
	matched := make([]int, 0, 1)
	for i, resource := range resources {
		if used[i] {
			continue
		}
		if !constraintMatchesTarget(resource, target) {
			continue
		}
		matched = append(matched, i)
	}
	switch len(matched) {
	case 0:
		return -1, errors.NewCode(errors.Forbidden, "write constraint does not authorize the target resource").
			WithContext("target", formatConstraintTarget(target))
	case 1:
		return matched[0], nil
	default:
		if constraintResourcesEquivalent(resources, matched) {
			return matched[0], nil
		}
		return -1, errors.NewCode(errors.InvalidInput, "write constraint matches multiple target resources").
			WithContext("target", formatConstraintTarget(target))
	}
}

func constraintMatchesTarget(resource ResourceConstraint, target ResourceBoundary) bool {
	if target.Kind != "" && resource.Kind != target.Kind {
		return false
	}
	if target.ID != "" && resource.ResourceID != target.ID {
		return false
	}
	if target.GlobalScope && !resource.GlobalScope {
		return false
	}
	if !target.GlobalScope && !resource.GlobalScope && target.ManagedScopeID != 0 && resource.ManagedScopeID != target.ManagedScopeID {
		return false
	}
	// target 显式声明 TenantID 时，约束必须精确匹配——空 TenantID 的约束不再
	// 被视作 "任意 tenant 通配"，防止租户隔离被绕过。
	if target.TenantID != "" && resource.TenantID != target.TenantID {
		return false
	}
	if target.Revision != "" && resource.Revision != target.Revision {
		return false
	}
	return true
}

func constraintResourcesEquivalent(resources []ResourceConstraint, indexes []int) bool {
	if len(indexes) <= 1 {
		return true
	}
	first := normalizeResourceConstraint(resources[indexes[0]])
	for _, index := range indexes[1:] {
		if !constraintResourceEqual(normalizeResourceConstraint(resources[index]), first) {
			return false
		}
	}
	return true
}

func constraintResourceEqual(left, right ResourceConstraint) bool {
	return left.Kind == right.Kind &&
		left.ResourceID == right.ResourceID &&
		left.ManagedScopeID == right.ManagedScopeID &&
		left.GlobalScope == right.GlobalScope &&
		left.TenantID == right.TenantID &&
		left.Revision == right.Revision
}

func formatConstraintTarget(target ResourceBoundary) string {
	target = normalizeResourceBoundary(target)
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
	if target.OwnerID != "" {
		parts = append(parts, "owner_id="+target.OwnerID)
	}
	if target.Revision != "" {
		parts = append(parts, "revision="+target.Revision)
	}
	if len(parts) == 0 {
		return "<empty>"
	}
	return strings.Join(parts, ",")
}
