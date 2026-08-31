package scoped

import (
	"context"
	"strings"

	"gochen/errors"
)

// ResourceConstraint 表达针对单个资源的写入约束。
type ResourceConstraint struct {
	Kind           string
	ResourceID     string
	ManagedScopeID int64
	GlobalScope    bool
	TenantID       string
	Revision       string
}

// WriteConstraint 表达一次写入被允许后的显式边界。
//
// 该值**不进入任何 Application 公共接口**，只经受控通道（见 provider.go）
// 传递给 Repo，由 Repo 合并进原子 WHERE。
type WriteConstraint struct {
	Resources []ResourceConstraint
}

// IsEmpty 判断约束是否未授权任何资源。
//
// 空约束意味着"没有任何资源被授权写入"，写路径必须据此拒绝，
// 绝不解释为"无约束 → 放行"。
func (c WriteConstraint) IsEmpty() bool { return len(c.Resources) == 0 }

// Normalize 返回规范化后的写约束。
func (c WriteConstraint) Normalize() WriteConstraint {
	if len(c.Resources) == 0 {
		c.Resources = nil
		return c
	}
	out := make([]ResourceConstraint, 0, len(c.Resources))
	for _, resource := range c.Resources {
		resource.Kind = strings.TrimSpace(resource.Kind)
		resource.ResourceID = strings.TrimSpace(resource.ResourceID)
		resource.TenantID = strings.TrimSpace(resource.TenantID)
		resource.Revision = strings.TrimSpace(resource.Revision)
		if resource.ManagedScopeID < 0 {
			resource.ManagedScopeID = 0
		}
		if resource.GlobalScope {
			resource.ManagedScopeID = 0
		}
		out = append(out, resource)
	}
	c.Resources = out
	return c
}

// Single 在约束恰好授权一个资源时返回该资源。
//
// 单条写路径要求约束精确对应一个目标；多于一个说明装配有误。
func (c WriteConstraint) Single() (ResourceConstraint, bool) {
	normalized := c.Normalize()
	if len(normalized.Resources) != 1 {
		return ResourceConstraint{}, false
	}
	return normalized.Resources[0], true
}

// DataScope 把约束授权的资源范围投影为 DataScope。
//
// 用于读路径复用同一份授权结果；无授权资源时返回 ScopeDenyAll。
func (c WriteConstraint) DataScope() DataScope {
	normalized := c.Normalize()
	if len(normalized.Resources) == 0 {
		return DenyAll()
	}
	scopeIDs := make([]int64, 0, len(normalized.Resources))
	for _, resource := range normalized.Resources {
		if resource.GlobalScope {
			return Global()
		}
		if resource.ManagedScopeID > 0 {
			scopeIDs = append(scopeIDs, resource.ManagedScopeID)
		}
	}
	return Filtered(scopeIDs...)
}

// ScopedContext 把单资源约束的数据范围绑定到 ctx。
//
// 要求约束恰好授权一个资源；全局资源或无范围时原样返回 ctx。
func (c WriteConstraint) ScopedContext(ctx context.Context) (context.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	resource, ok := c.Single()
	if !ok {
		return nil, errors.NewCode(errors.InvalidInput,
			"write constraint must contain exactly one resource to derive scoped context").
			WithContext("resource_count", len(c.Normalize().Resources))
	}
	if resource.GlobalScope || resource.ManagedScopeID == 0 {
		return ctx, nil
	}
	return WithDataScope(ctx, Filtered(resource.ManagedScopeID))
}
