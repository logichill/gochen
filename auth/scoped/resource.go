package scoped

import (
	"reflect"
	"strings"
	"sync"

	"gochen/errors"
)

// Resource 表达授权引擎可理解的统一资源描述（原 domain/access.ResourceBoundary）。
type Resource struct {
	// Kind 资源类型，例 "order"。
	Kind string
	// ID 资源标识。
	ID string
	// ManagedScopeID 资源所属数据范围；GlobalScope 为 true 时无意义。
	ManagedScopeID int64
	// GlobalScope 表示资源不受 managed scope 约束（平台级资源）。
	GlobalScope bool
	// TenantID 结构化租户归属；授权判定不依赖 OwnerID 字符串前缀。
	TenantID string
	// OwnerID 资源归属主体。
	OwnerID string
	// Revision 乐观锁版本，用于写路径原子求交。
	Revision string
}

// Normalize 清理空白字段并统一 global scope 表示。
func (r Resource) Normalize() Resource {
	r.Kind = strings.TrimSpace(r.Kind)
	r.ID = strings.TrimSpace(r.ID)
	r.TenantID = strings.TrimSpace(r.TenantID)
	r.OwnerID = strings.TrimSpace(r.OwnerID)
	r.Revision = strings.TrimSpace(r.Revision)
	if r.ManagedScopeID < 0 {
		r.ManagedScopeID = 0
	}
	if r.GlobalScope {
		r.ManagedScopeID = 0
	}
	return r
}

// IResourceResolver 把业务对象转换为资源描述。
type IResourceResolver interface {
	Resolve(target any) (Resource, bool)
}

// ITypedResourceResolver 是带目标类型元数据的资源解析器。
type ITypedResourceResolver interface {
	IResourceResolver
	TargetType() reflect.Type
}

// ResourceResolverFunc 允许用函数直接实现 IResourceResolver。
type ResourceResolverFunc func(target any) (Resource, bool)

// Resolve 让普通函数可以直接作为资源解析器使用。
func (f ResourceResolverFunc) Resolve(target any) (Resource, bool) {
	if f == nil {
		return Resource{}, false
	}
	return f(target)
}

type typedResourceResolver[T any] struct {
	resolver func(target T) (Resource, bool)
}

// Resolve 只在入参能断言为目标类型时才执行具体解析逻辑。
func (r typedResourceResolver[T]) Resolve(target any) (Resource, bool) {
	if r.resolver == nil {
		return Resource{}, false
	}
	typed, ok := target.(T)
	if !ok {
		return Resource{}, false
	}
	resource, resolved := r.resolver(typed)
	if !resolved {
		return Resource{}, false
	}
	return resource.Normalize(), true
}

func (r typedResourceResolver[T]) TargetType() reflect.Type { return reflect.TypeFor[T]() }

// TypedResourceResolver 为指定目标类型创建带类型元数据的资源解析器。
func TypedResourceResolver[T any](resolver func(target T) (Resource, bool)) ITypedResourceResolver {
	return typedResourceResolver[T]{resolver: resolver}
}

// ResourceRegistry 按注册顺序组合多个资源解析器。
//
// Register 可在启动期完成后继续调用（例如运行期动态注册），
// 因此内部有锁：注册与解析并发时不需要调用方自己加锁。
type ResourceRegistry struct {
	mu        sync.RWMutex
	resolvers []IResourceResolver
}

// NewResourceRegistry 创建按注册顺序尝试解析器的注册表。
func NewResourceRegistry(resolvers ...IResourceResolver) *ResourceRegistry {
	registry := &ResourceRegistry{}
	for _, resolver := range resolvers {
		registry.Register(resolver)
	}
	return registry
}

// Register 追加一个资源解析器。
//
// 注册表不做冲突裁决；多个解析器命中同一目标时先注册者优先。
func (r *ResourceRegistry) Register(resolver IResourceResolver) {
	if r == nil || resolver == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolvers = append(r.resolvers, resolver)
}

// Resolve 依次尝试各解析器，返回首个成功解析出的资源。
func (r *ResourceRegistry) Resolve(target any) (Resource, bool) {
	if r == nil {
		return Resource{}, false
	}
	r.mu.RLock()
	resolvers := r.resolvers
	r.mu.RUnlock()
	for _, resolver := range resolvers {
		if resolver == nil {
			continue
		}
		if resource, ok := resolver.Resolve(target); ok {
			return resource.Normalize(), true
		}
	}
	return Resource{}, false
}

// ResolveResources 批量把目标对象转换成规范化资源描述。
//
// 允许混用已是 Resource 的对象与待解析的业务对象；
// 任一目标无法解析即立即返回错误，绝不忽略失败项（fail-closed）。
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
			resources = append(resources, typed.Normalize())
			continue
		case *Resource:
			if typed == nil {
				return nil, errors.NewCode(errors.InvalidInput, "resource target cannot be nil")
			}
			resources = append(resources, typed.Normalize())
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
		resources = append(resources, resource.Normalize())
	}
	return resources, nil
}
