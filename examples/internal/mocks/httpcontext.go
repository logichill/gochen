// Package mocks 提供示例中使用的模拟组件
package mocks

import (
	"context"
	"log"
	"net/http"
	"net/url"

	httpx "gochen/http"
	hbasic "gochen/http/basic"
)

// MockHttpContext 模拟 HTTP 上下文
type MockHttpContext struct {
	Method  string
	Path    string
	Headers map[string]string
	Params  map[string]string
	Query   map[string]string
}

// NewMockHttpContext 创建模拟 HTTP 上下文
func NewMockHttpContext(method, path string) *MockHttpContext {
	return &MockHttpContext{
		Method:  method,
		Path:    path,
		Headers: make(map[string]string),
		Params:  make(map[string]string),
		Query:   make(map[string]string),
	}
}

func (m *MockHttpContext) GetHeader(key string) string {
	if m.Headers == nil {
		return ""
	}
	return m.Headers[key]
}

func (m *MockHttpContext) GetMethod() string {
	return m.Method
}

func (m *MockHttpContext) GetPath() string {
	return m.Path
}

func (m *MockHttpContext) GetParam(key string) string {
	if m.Params == nil {
		return ""
	}
	return m.Params[key]
}

func (m *MockHttpContext) GetQuery(key string) string {
	if m.Query == nil {
		return ""
	}
	return m.Query[key]
}

func (m *MockHttpContext) GetContext() httpx.IRequestContext {
	return hbasic.NewRequestContext(context.Background())
}

func (m *MockHttpContext) SetContext(ctx httpx.IRequestContext) {
}

func (m *MockHttpContext) ShouldBindJSON(v any) error {
	return nil
}

func (m *MockHttpContext) JSON(code int, data any) error {
	log.Printf("JSON Response: %d - %+v", code, data)
	return nil
}

func (m *MockHttpContext) String(code int, text string) error {
	log.Printf("String Response: %d - %s", code, text)
	return nil
}

func (m *MockHttpContext) Data(code int, contentType string, data []byte) error {
	log.Printf("Data Response: %d - %s", code, contentType)
	return nil
}

func (m *MockHttpContext) SetStatus(code int) {
	log.Printf("Set status: %d", code)
}

func (m *MockHttpContext) SetHeader(key, value string) {
	log.Printf("Set header: %s = %s", key, value)
	if m.Headers == nil {
		m.Headers = make(map[string]string)
	}
	m.Headers[key] = value
}

func (m *MockHttpContext) Abort() {
	log.Printf("Request aborted")
}

func (m *MockHttpContext) AbortWithStatus(code int) {
	log.Printf("Request aborted with status: %d", code)
}

func (m *MockHttpContext) AbortWithStatusJSON(code int, jsonObj any) {
	log.Printf("Request aborted with status %d and JSON: %+v", code, jsonObj)
}

func (m *MockHttpContext) BindJSON(obj any) error {
	return m.ShouldBindJSON(obj)
}

func (m *MockHttpContext) BindQuery(obj any) error {
	return nil
}

func (m *MockHttpContext) IsAborted() bool {
	return false
}

func (m *MockHttpContext) Set(key string, value any) {
	// 简化实现
}

func (m *MockHttpContext) Get(key string) (any, bool) {
	return nil, false
}

func (m *MockHttpContext) MustGet(key string) any {
	return nil
}

func (m *MockHttpContext) GetRaw() any {
	return m
}

func (m *MockHttpContext) GetBody() ([]byte, error) {
	return nil, nil
}

func (m *MockHttpContext) GetRequest() *http.Request {
	return nil
}

func (m *MockHttpContext) ClientIP() string {
	return "127.0.0.1"
}

func (m *MockHttpContext) UserAgent() string {
	return "MockClient"
}

func (m *MockHttpContext) GetQueryParams() url.Values {
	params := url.Values{}
	for k, v := range m.Query {
		params[k] = []string{v}
	}
	return params
}

// MockHttpContextWithHeaders 模拟带请求头的 HTTP 上下文
type MockHttpContextWithHeaders struct {
	*MockHttpContext
	headers map[string]string
}

// NewMockHttpContextWithHeaders 创建带请求头的模拟 HTTP 上下文
func NewMockHttpContextWithHeaders(method, path string) *MockHttpContextWithHeaders {
	return &MockHttpContextWithHeaders{
		MockHttpContext: NewMockHttpContext(method, path),
		headers:         make(map[string]string),
	}
}

func (c *MockHttpContextWithHeaders) GetHeader(key string) string {
	if c.headers == nil {
		return ""
	}
	return c.headers[key]
}

// SimpleDelegateHandler 简单委托处理器
type SimpleDelegateHandler func(httpx.IHttpContext) error
