// package http 提供简化的 HTTP 接口，遵循接口隔离原则
package http

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// IRequestReader 请求读取接口 - 只负责读取请求数据
type IRequestReader interface {
	// 基础信息
	GetMethod() string
	GetPath() string
	GetHeader(key string) string
	GetQuery(key string) string
	GetParam(key string) string
	GetQueryParams() url.Values

	// 请求体
	GetBody() ([]byte, error)
	GetRequest() *http.Request

	// 客户端信息
	ClientIP() string
	UserAgent() string
}

// IRequestBinder 请求绑定接口 - 只负责数据绑定
type IRequestBinder interface {
	BindJSON(obj any) error
	BindQuery(obj any) error
	ShouldBindJSON(obj any) error
}

// 预定义上下文键（供实现与调用方共享）
// 使用自定义类型 contextKey（定义在 tracing.go）避免与其他包的字符串 key 冲突
const (
	TraceIDKey       contextKey = "trace_id"
	UserIDKey        contextKey = "user_id"
	RequestIDKey     contextKey = "request_id"
	TenantIDKey      contextKey = "tenant_id"
	SessionIDKey     contextKey = "session_id"
	IPAddressKey     contextKey = "ip_address"
	UserAgentKey     contextKey = "user_agent"
	CorrelationIDKey contextKey = "correlation_id"
)

// IRequestContext 请求上下文接口（实现见 httpx/basic）
type IRequestContext interface {
	context.Context

	GetTraceID() string
	GetUserID() int64
	GetRequestID() string
	GetTenantID() string
	GetSessionID() string
	GetIPAddress() string
	GetUserAgent() string
	GetCorrelationID() string

	WithValue(key any, value any) IRequestContext
	WithTimeout(timeout time.Duration) (IRequestContext, context.CancelFunc)
	WithCancel() (IRequestContext, context.CancelFunc)
	WithDeadline(deadline time.Time) (IRequestContext, context.CancelFunc)
	Clone() IRequestContext
}
