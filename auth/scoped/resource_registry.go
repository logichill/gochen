package scoped

import (
	"sync"
)

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
	// 持读锁做元素级快照（而不是仅拷贝切片头）：锁外只遍历快照数组，
	// 与并发 Register 的 append（容量未耗尽时原地写入底层数组）彻底隔离。
	r.mu.RLock()
	resolvers := make([]IResourceResolver, len(r.resolvers))
	copy(resolvers, r.resolvers)
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
