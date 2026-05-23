// Package httpx 提供简化的 HTTP 接口，遵循接口隔离原则。
package httpx

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// IRequestReader 请求读取接口 - 只负责读取请求数据。
type IRequestReader interface {
	// 基础信息
	Method() string
	Path() string
	Header(key string) string
	Query(key string) string
	Param(key string) string
	QueryParams() url.Values

	// 请求体
	Body() ([]byte, error)
	Request() *http.Request

	// 客户端信息
	ClientIP() string
	UserAgent() string
}

// MaxBodySizeKey 用于通过 IContext.Set 在请求上下文中声明“最大请求体大小”（单位：bytes）。
//
// 说明：
// - 这是一个“实现可选”的约定键：例如 `httpx/nethttp` 会读取该值并在读取 Body 时强制限制；
// - 对于不支持该能力的 IContext 实现，设置该键不会产生效果；
// - 该键用于 API 层/路由层对单个端点做细粒度限制（而不是全局 WebConfig 级别配置）。
const MaxBodySizeKey = "gochen.http.max_body_size"

// DefaultMaxBodySizeBytes 默认请求体大小上限（bytes）。
//
// 说明：
//   - 该默认值用于 `httpx/nethttp` 的安全兜底：当调用方未显式设置 `MaxBodySizeKey` 时，
//     仍会启用一个合理的上限，避免无界读取导致内存放大。
//   - 若需要放大或放开限制：在请求上下文显式 `Set(MaxBodySizeKey, <bytes>)`；
//     若需显式关闭限制：设置为 0（opt-out）。
const DefaultMaxBodySizeBytes int64 = 10 << 20 // 10MB

// IRequestBinder 请求绑定接口 - 只负责数据绑定。
type IRequestBinder interface {
	BindJSON(obj any) error
	BindQuery(obj any) error
	ShouldBindJSON(obj any) error
}

// IRequestContext 请求上下文接口。
//
// 说明：
// - 该接口只表达 HTTP 请求链路中的 context 派生能力；
// - tenant/trace/request/operator/user/session 等运行时语义应通过 `gochen/contextx` 读取。
type IRequestContext interface {
	context.Context

	WithContext(ctx context.Context) IRequestContext
	WithValue(key any, value any) IRequestContext
	WithTimeout(timeout time.Duration) (IRequestContext, context.CancelFunc)
	WithCancel() (IRequestContext, context.CancelFunc)
	WithDeadline(deadline time.Time) (IRequestContext, context.CancelFunc)
	Clone() IRequestContext
}
