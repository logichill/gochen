package auth

import (
	"fmt"
	"reflect"
	"strings"

	"gochen/errors"
)

// Resource 表达授权引擎可理解的统一资源描述。
//
// 字段语义：
//   - GlobalScope=true 表示资源不受 managed scope 约束（典型如 platform 级资源）。
//     此时 ManagedScopeID 必须为 0；若原数据有值，normalize 阶段会将其清零，
//     避免 "ManagedScopeID=0 且非 global" 与 "global" 被混为一谈。
//   - TenantID 是结构化的租户归属字段，授权判定应读取该字段而非从 OwnerID
//     的字符串前缀推断。OwnerID 仅用于业务归属语义。
type Resource struct {
	Kind           string
	ID             string
	ManagedScopeID int64
	GlobalScope    bool
	TenantID       string
	OwnerID        string
	Revision       string
}

// IResourceResolver 定义资源转换能力接口。
type IResourceResolver interface {
	Resolve(target any) (Resource, bool)
}

// ITypedResourceResolver 定义带目标类型元数据的资源解析器。
type ITypedResourceResolver interface {
	IResourceResolver
	TargetType() reflect.Type
}

// ResourceResolverFunc 允许使用函数直接实现 IResourceResolver。
type ResourceResolverFunc func(target any) (Resource, bool)

// Resolve 让普通函数可以直接作为资源解析器使用。
func (f ResourceResolverFunc) Resolve(target any) (Resource, bool) {
	return f(target)
}

type typedResourceResolver[T any] struct {
	resolver func(target T) (Resource, bool)
}

// Resolve 只在入参能够断言为目标类型时才执行具体解析逻辑。
func (r typedResourceResolver[T]) Resolve(target any) (Resource, bool) {
	typed, ok := target.(T)
	if !ok {
		return Resource{}, false
	}
	resource, resolved := r.resolver(typed)
	if !resolved {
		return Resource{}, false
	}
	return normalizeResource(resource), true
}

func (r typedResourceResolver[T]) TargetType() reflect.Type {
	return reflect.TypeFor[T]()
}

// ResourceRegistry 按顺序组合多个资源解析器。
type ResourceRegistry struct {
	resolvers []IResourceResolver
}

// NewResourceRegistry 创建一个按注册顺序尝试解析器的注册表。
func NewResourceRegistry(resolvers ...IResourceResolver) *ResourceRegistry {
	registry := &ResourceRegistry{}
	for _, resolver := range resolvers {
		registry.Register(resolver)
	}
	return registry
}

// Register 追加一个资源解析器。
//
// 资源注册表不做冲突裁决；若多个解析器都能命中同一目标，先注册的解析器优先。
func (r *ResourceRegistry) Register(resolver IResourceResolver) {
	if r == nil || resolver == nil {
		return
	}
	r.resolvers = append(r.resolvers, resolver)
}

// Resolve 依次尝试各解析器，并返回首个成功解析出的资源。
func (r *ResourceRegistry) Resolve(target any) (Resource, bool) {
	if r == nil {
		return Resource{}, false
	}
	for _, resolver := range r.resolvers {
		if resolver == nil {
			continue
		}
		resource, ok := resolver.Resolve(target)
		if !ok {
			continue
		}
		return normalizeResource(resource), true
	}
	return Resource{}, false
}

// TypedResourceResolver 为指定目标类型创建一个带类型元数据的资源解析器。
func TypedResourceResolver[T any](resolver func(target T) (Resource, bool)) ITypedResourceResolver {
	return typedResourceResolver[T]{resolver: resolver}
}

// ResolveResources 批量把目标对象转换成规范化后的资源描述。
//
// 它允许调用方混用已是 `Resource` 的对象和待解析的业务对象；只要其中任一目标
// 无法解析，就立即返回错误而不是忽略失败项。
func ResolveResources(resolver IResourceResolver, targets ...any) ([]Resource, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	resources := make([]Resource, 0, len(targets))
	for _, target := range targets {
		if target == nil {
			return nil, errors.NewCode(errors.InvalidInput, "resource target cannot be nil")
		}
		switch typed := target.(type) {
		case Resource:
			resources = append(resources, normalizeResource(typed))
			continue
		case *Resource:
			if typed == nil {
				return nil, errors.NewCode(errors.InvalidInput, "resource target cannot be nil")
			}
			resources = append(resources, normalizeResource(*typed))
			continue
		}
		if resolver == nil {
			return nil, errors.NewCode(errors.InvalidInput, "resource resolver is required")
		}
		resource, ok := resolver.Resolve(target)
		if !ok {
			return nil, errors.NewCode(errors.InvalidInput, "resource target is not registered").
				WithContext("target_type", reflect.TypeOf(target).String())
		}
		resources = append(resources, normalizeResource(resource))
	}
	return resources, nil
}

// normalizeResource 清理空白字段，并统一处理 global scope 的表示方式。
func normalizeResource(resource Resource) Resource {
	resource.Kind = strings.TrimSpace(resource.Kind)
	resource.ID = strings.TrimSpace(resource.ID)
	resource.ManagedScopeID = NormalizePositiveID(resource.ManagedScopeID)
	resource.TenantID = strings.TrimSpace(resource.TenantID)
	resource.OwnerID = strings.TrimSpace(resource.OwnerID)
	resource.Revision = strings.TrimSpace(resource.Revision)
	if resource.GlobalScope {
		resource.ManagedScopeID = 0
	}
	return resource
}

// formatTargetType 返回目标对象的可读类型名，供错误上下文使用。
func formatTargetType(target any) string {
	if target == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T", target)
}
