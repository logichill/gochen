// Package mocks 提供示例中使用的模拟组件
package mocks

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sync"

	"gochen/errors"
	"gochen/httpx"
)

// MockHttpContext 是示例里使用的最小 HTTP 上下文桩实现。
type MockHttpContext struct {
	HTTPMethod  string
	RequestPath string
	Headers     map[string]string
	Params      map[string]string
	QueryValues map[string]string

	mu     sync.RWMutex
	values map[string]httpx.ContextValue
}

// NewMockHttpContext 创建一个带独立 header/param/query 存储的 mock 上下文。
func NewMockHttpContext(method, path string) *MockHttpContext {
	return &MockHttpContext{
		HTTPMethod:  method,
		RequestPath: path,
		Headers:     make(map[string]string),
		Params:      make(map[string]string),
		QueryValues: make(map[string]string),
		values:      make(map[string]httpx.ContextValue),
	}
}

// Header 返回 mock 请求头中的指定值。
func (m *MockHttpContext) Header(key string) string {
	if m.Headers == nil {
		return ""
	}
	return m.Headers[key]
}

// Method 返回 mock 请求方法。
func (m *MockHttpContext) Method() string {
	return m.HTTPMethod
}

// Path 返回 mock 请求路径。
func (m *MockHttpContext) Path() string {
	return m.RequestPath
}

// Param 返回 mock 路由参数中的指定值。
func (m *MockHttpContext) Param(key string) string {
	if m.Params == nil {
		return ""
	}
	return m.Params[key]
}

// Query 返回 mock 查询参数中的指定值。
func (m *MockHttpContext) Query(key string) string {
	if m.QueryValues == nil {
		return ""
	}
	return m.QueryValues[key]
}

// RequestContext 返回一个基于 background context 构造的请求上下文。
func (m *MockHttpContext) RequestContext() httpx.IRequestContext {
	ctx, err := httpx.NewRequestContext(context.Background())
	if err != nil {
		return nil
	}
	return ctx
}

// SetContext 在 mock 中保留为空实现。
func (m *MockHttpContext) SetContext(ctx httpx.IRequestContext) {
}

// ShouldBindJSON 在 mock 中保留为空实现，只验证调用路径可达。
func (m *MockHttpContext) ShouldBindJSON(v any) error {
	_ = v
	return nil
}

// JSON 序列化响应体并把结果写入日志，便于示例观察返回值。
func (m *MockHttpContext) JSON(code int, data httpx.JSONBody) error {
	serialized, err := json.Marshal(data)
	if err != nil {
		return err
	}
	log.Printf("JSON Response: %d - %s", code, string(serialized))
	return nil
}

// String 记录一条纯文本响应日志。
func (m *MockHttpContext) String(code int, text string) error {
	log.Printf("String Response: %d - %s", code, text)
	return nil
}

// Data 记录一条原始数据响应日志。
func (m *MockHttpContext) Data(code int, contentType string, data []byte) error {
	log.Printf("Data Response: %d - %s", code, contentType)
	return nil
}

// SetStatus 在 mock 中只记录状态码。
func (m *MockHttpContext) SetStatus(code int) {
	log.Printf("Set status: %d", code)
}

// SetHeader 记录并保存响应头。
func (m *MockHttpContext) SetHeader(key, value string) {
	log.Printf("Set header: %s = %s", key, value)
	if m.Headers == nil {
		m.Headers = make(map[string]string)
	}
	m.Headers[key] = value
}

// Abort 标记请求为已终止（测试 mock，仅记录日志）。
func (m *MockHttpContext) Abort() {
	log.Printf("Request aborted")
}

// AbortWithStatus 在 mock 中只记录终止状态码。
func (m *MockHttpContext) AbortWithStatus(code int) {
	log.Printf("Request aborted with status: %d", code)
}

// AbortWithStatusJSON 记录一条带 JSON 载荷的终止日志。
func (m *MockHttpContext) AbortWithStatusJSON(code int, jsonObj httpx.JSONBody) {
	serialized, err := json.Marshal(jsonObj)
	if err != nil {
		log.Printf("Request aborted with status %d and JSON marshal failed: %v", code, err)
		return
	}
	log.Printf("Request aborted with status %d and JSON: %s", code, string(serialized))
}

// BindJSON 复用 ShouldBindJSON。
func (m *MockHttpContext) BindJSON(obj any) error {
	return m.ShouldBindJSON(obj)
}

// BindQuery 在 mock 中保留为空实现。
func (m *MockHttpContext) BindQuery(obj any) error {
	_ = obj
	return nil
}

// IsAborted 在当前 mock 中始终返回 false。
func (m *MockHttpContext) IsAborted() bool {
	return false
}

// Set 保存一项上下文值。
func (m *MockHttpContext) Set(key string, value httpx.ContextValue) {
	if key == "" {
		return
	}
	m.mu.Lock()
	if m.values == nil {
		m.values = make(map[string]httpx.ContextValue)
	}
	m.values[key] = value
	m.mu.Unlock()
}

// Get 读取一项上下文值。
func (m *MockHttpContext) Get(key string) (httpx.ContextValue, bool) {
	if key == "" {
		return httpx.ContextValue{}, false
	}
	m.mu.RLock()
	v, ok := m.values[key]
	m.mu.RUnlock()
	return v, ok
}

// Required 获取指定 key 的值；当 key 不存在时返回 error（不再 panic）。
func (m *MockHttpContext) Required(key string) (httpx.ContextValue, error) {
	if key == "" {
		return httpx.ContextValue{}, errors.NewCode(errors.InvalidInput, "key cannot be empty")
	}
	if v, ok := m.Get(key); ok {
		return v, nil
	}
	return httpx.ContextValue{}, errors.NewCode(errors.Internal, "missing required context value").WithContext("key", key)
}

// Body 在 mock 中始终返回空请求体。
func (m *MockHttpContext) Body() ([]byte, error) {
	return nil, nil
}

// Request 在当前 mock 中不提供原始 http.Request。
func (m *MockHttpContext) Request() *http.Request {
	return nil
}

// ClientIP 返回固定的本地回环地址。
func (m *MockHttpContext) ClientIP() string {
	return "127.0.0.1"
}

// UserAgent 返回固定的 mock User-Agent。
func (m *MockHttpContext) UserAgent() string {
	return "MockClient"
}

// QueryParams 把内部查询参数转换为 `url.Values`。
func (m *MockHttpContext) QueryParams() url.Values {
	params := url.Values{}
	for k, v := range m.QueryValues {
		params[k] = []string{v}
	}
	return params
}

// MockHttpContextWithHeaders 在基础 mock 上额外挂一份 headers 集合。
type MockHttpContextWithHeaders struct {
	*MockHttpContext
	headers map[string]string
}

// NewMockHttpContextWithHeaders 创建一个带独立 header 集合的 mock 上下文。
func NewMockHttpContextWithHeaders(method, path string) *MockHttpContextWithHeaders {
	return &MockHttpContextWithHeaders{
		MockHttpContext: NewMockHttpContext(method, path),
		headers:         make(map[string]string),
	}
}

// Header 优先从扩展 header 集合里读取指定值。
func (c *MockHttpContextWithHeaders) Header(key string) string {
	if c.headers == nil {
		return ""
	}
	return c.headers[key]
}

// SimpleDelegateHandler 便于把普通函数直接适配为示例处理器。
type SimpleDelegateHandler func(httpx.IContext) error
