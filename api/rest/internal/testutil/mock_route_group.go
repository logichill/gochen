package testutil

import "gochen/httpx"

// MockRouteGroup 用于测试的路由分组 stub。
type MockRouteGroup struct {
	Handlers map[string]httpx.Handler
}

// NewMockRouteGroup 创建一个用于测试注册行为的空路由分组。
func NewMockRouteGroup() *MockRouteGroup {
	return &MockRouteGroup{
		Handlers: make(map[string]httpx.Handler),
	}
}

// key 生成内部使用的 `METHOD path` 索引键。
func (m *MockRouteGroup) key(method, path string) string { return method + " " + path }

// GET 记录一条 GET 路由。
func (m *MockRouteGroup) GET(path string, handler httpx.Handler) httpx.IRouteGroup {
	m.Handlers[m.key("GET", path)] = handler
	return m
}

// POST 记录一条 POST 路由。
func (m *MockRouteGroup) POST(path string, handler httpx.Handler) httpx.IRouteGroup {
	m.Handlers[m.key("POST", path)] = handler
	return m
}

// PUT 记录一条 PUT 路由。
func (m *MockRouteGroup) PUT(path string, handler httpx.Handler) httpx.IRouteGroup {
	m.Handlers[m.key("PUT", path)] = handler
	return m
}

func (m *MockRouteGroup) DELETE(path string, handler httpx.Handler) httpx.IRouteGroup {
	m.Handlers[m.key("DELETE", path)] = handler
	return m
}

// PATCH 记录一条 PATCH 路由。
func (m *MockRouteGroup) PATCH(path string, handler httpx.Handler) httpx.IRouteGroup {
	m.Handlers[m.key("PATCH", path)] = handler
	return m
}

// HEAD 记录一条 HEAD 路由。
func (m *MockRouteGroup) HEAD(path string, handler httpx.Handler) httpx.IRouteGroup {
	m.Handlers[m.key("HEAD", path)] = handler
	return m
}

// OPTIONS 记录一条 OPTIONS 路由。
func (m *MockRouteGroup) OPTIONS(path string, handler httpx.Handler) httpx.IRouteGroup {
	m.Handlers[m.key("OPTIONS", path)] = handler
	return m
}

// Group 在测试 stub 中直接复用当前分组实例。
func (m *MockRouteGroup) Group(prefix string) httpx.IRouteGroup {
	_ = prefix
	return m
}

// Use 在测试 stub 中忽略中间件注册，只保留链式调用能力。
func (m *MockRouteGroup) Use(middleware ...httpx.Middleware) httpx.IRouteGroup {
	_ = middleware
	return m
}

var _ httpx.IRouteGroup = (*MockRouteGroup)(nil)
