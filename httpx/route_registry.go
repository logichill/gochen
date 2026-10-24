package httpx

import (
	"sort"
	"strings"
	"sync"
)

// RouteConflict 表示 method+path 发生重复注册。
type RouteConflict struct {
	Method string
	Path   string
	Count  int
}

// IRouteRegistry 暴露路由注册的冲突信息（用于启动期 fail-fast 校验）。
type IRouteRegistry interface {
	RouteConflicts() []RouteConflict
}

func WithRouteRegistry(inner IServer) IServer {
	if inner == nil {
		return nil
	}
	if _, ok := inner.(IRouteRegistry); ok {
		return inner
	}
	return &routeRegistryServer{inner: inner, reg: newRouteRegistry()}
}

type routeKey struct {
	method string
	path   string
}

type routeRegistry struct {
	mu     sync.Mutex
	counts map[routeKey]int
}

// newRouteRegistry 创建路由注册表。
func newRouteRegistry() *routeRegistry {
	return &routeRegistry{counts: make(map[routeKey]int, 128)}
}

// add 累计 (METHOD, normalizedPath) 注册次数；用于 conflicts 检测重复路由。
func (r *routeRegistry) add(method, path string) {
	method = strings.TrimSpace(strings.ToUpper(method))
	path = NormalizeRoutePath(path)
	if method == "" || path == "" {
		return
	}

	r.mu.Lock()
	r.counts[routeKey{method: method, path: path}]++
	r.mu.Unlock()
}

// conflicts 返回所有注册次数 > 1 的路由（按 method+path 维度认定为冲突）。
func (r *routeRegistry) conflicts() []RouteConflict {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]RouteConflict, 0)
	for k, c := range r.counts {
		if c > 1 {
			out = append(out, RouteConflict{Method: k.method, Path: k.path, Count: c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}
