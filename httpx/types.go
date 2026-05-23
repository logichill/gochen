package httpx

import (
	"crypto/tls"
	"time"
)

// ResponseMessage 表示统一的 HTTP JSON 响应消息。
type ResponseMessage struct {
	// Code 表示业务结果码（成功默认 "ok"，失败建议使用 errors.ErrorCode 的字符串值）。
	Code string `json:"code"`
	// Message 表示对调用方可见的结果消息。
	Message string `json:"message"`
	// Data 表示成功场景的业务载荷。
	Data any `json:"data,omitempty"`
	// Details 表示可选的错误详情（应避免包含敏感信息）。
	Details string `json:"details,omitempty"`
	// Extra 表示可选的结构化附加信息（如部分结果、OAuth 描述等）。
	Extra map[string]any `json:"extra,omitempty"`
	// TraceID 表示链路追踪标识（用于客户端与服务端日志关联）。
	TraceID string `json:"trace_id,omitempty"`
	// RequestID 表示请求唯一标识（用于客户端与服务端日志关联）。
	RequestID string `json:"request_id,omitempty"`
}

// NewResponseMessage 创建统一响应消息。
func NewResponseMessage(code, message string) *ResponseMessage {
	return &ResponseMessage{
		Code:    code,
		Message: message,
	}
}

// NewSuccessMessage 创建成功响应消息。
func NewSuccessMessage(data any) *ResponseMessage {
	return &ResponseMessage{
		Code:    "ok",
		Message: "success",
		Data:    data,
	}
}

// WebConfig HTTP 服务基础配置。
type WebConfig struct {
	Host string `json:"host" yaml:"host" default:"0.0.0.0"`
	Port int    `json:"port" yaml:"port" default:"8082" validate:"min=1024,max=65535"`
	Mode string `json:"mode" yaml:"mode" default:"release" validate:"required,oneof=debug release test"`
	// EnableRequestLog 控制适配层是否启用框架自带访问日志。
	//
	// 说明：
	// - nil 表示由具体适配层按既有默认行为决定；
	// - 显式 false 可避免与业务自定义 access log 重复输出。
	EnableRequestLog *bool `json:"enable_request_log,omitempty" yaml:"enable_request_log,omitempty"`

	// SecurityLayer 表示该 HTTP 服务默认承载的“安全分层”语义（API/Web）。
	//
	// 说明：
	// - 该字段仅用于“语义标签 + 防误用约束”（例如 API 默认拒绝 Session 语义），不直接实现鉴权/CSRF 等能力；
	// - 对于需要更精细控制的场景（同一进程同时承载 API 与 Web），建议通过路由 group 分别挂载
	//   `httpx/middleware.SecurityLayer` 来覆盖该默认值。
	SecurityLayer SecurityLayer `json:"security_layer" yaml:"security_layer"`
	// AllowSession 控制是否允许 Session 语义（contextx.SessionID）。
	//
	// 说明：
	// - nil 表示按 SecurityLayer 取默认值：API -> false（默认拒绝），Web -> true；
	// - 该字段不提供 Session 的创建/验证/持久化能力，仅用于减少误用。
	AllowSession *bool `json:"allow_session" yaml:"allow_session"`
	// ReadHeaderTimeout 读取请求头超时（用于抵抗 slowloris/慢头攻击）。
	//
	// 说明：
	// - 安全默认：如果未设置，将使用 DefaultReadHeaderTimeout；
	// - 若需禁用（不建议），可显式设置为负值，normalize 时会归零。
	ReadHeaderTimeout time.Duration `json:"read_header_timeout" yaml:"read_header_timeout"`
	ReadTimeout       time.Duration `json:"read_timeout" yaml:"read_timeout" default:"30s"`
	WriteTimeout      time.Duration `json:"write_timeout" yaml:"write_timeout" default:"30s"`
	IdleTimeout       time.Duration `json:"idle_timeout" yaml:"idle_timeout" default:"60s"`
	// MaxHeaderBytes 最大请求头大小（用于防止超大 header 造成资源消耗放大）。
	//
	// 说明：
	// - 安全默认：如果未设置，将使用 DefaultMaxHeaderBytes。
	MaxHeaderBytes int `json:"max_header_bytes" yaml:"max_header_bytes"`
	// TrustedProxies 可信代理列表（IP 或 CIDR），用于决定是否信任 X-Forwarded-For/X-Real-IP 等代理头。
	//
	// 安全默认：当该字段为空时，不信任任何代理头，ClientIP() 仅返回 RemoteAddr。
	TrustedProxies []string `json:"trusted_proxies" yaml:"trusted_proxies"`

	// TLS
	TLSEnabled bool   `json:"tls_enabled" yaml:"tls_enabled"`
	CertFile   string `json:"cert_file" yaml:"cert_file"`
	KeyFile    string `json:"key_file" yaml:"key_file"`
	// TLSMinVersion TLS 最低版本。
	//
	// 说明：
	// - 仅在 TLSEnabled=true 时生效；
	// - 安全默认：未设置时将使用 TLS 1.2；
	// - 允许值（大小写不敏感）：\"1.2\"/\"1.3\"（也支持 \"tls1.2\"/\"tls1.3\"）。
	TLSMinVersion string `json:"tls_min_version" yaml:"tls_min_version"`
	// TLSConfig 允许以编程方式注入 tls.Config（用于 in-memory certs/GetCertificate 等）。
	//
	// 注意：
	// - 该字段不会被 json/yaml 反序列化；如需配置文件驱动，请使用 TLSMinVersion + CertFile/KeyFile。
	TLSConfig *tls.Config `json:"-" yaml:"-"`

	// CORS
	CORSEnabled      bool     `json:"cors_enabled" yaml:"cors_enabled" default:"true"`
	CORSAllowOrigins []string `json:"cors_allow_origins" yaml:"cors_allow_origins" default:"*"`
	CORSAllowMethods []string `json:"cors_allow_methods" yaml:"cors_allow_methods" default:"GET,POST,PUT,DELETE,PATCH,OPTIONS"`
	CORSAllowHeaders []string `json:"cors_allow_headers" yaml:"cors_allow_headers" default:"Origin,Content-Type,Authorization"`
}

// 默认超时配置（适用于未显式配置的开发/简单场景）：
// - 读/写超时：15 秒。
// - 空闲连接超时：60 秒。
const (
	// DefaultReadHeaderTimeout 定义相关常量。
	DefaultReadHeaderTimeout = 5 * time.Second
	// DefaultMaxHeaderBytes 定义相关常量。
	DefaultMaxHeaderBytes = 1 << 20 // 1MB（与 net/http 默认一致）
	// DefaultReadTimeout 定义相关常量。
	DefaultReadTimeout = 15 * time.Second
	// DefaultWriteTimeout 定义相关常量。
	DefaultWriteTimeout = 15 * time.Second
	// DefaultIdleTimeout 定义相关常量。
	DefaultIdleTimeout = 60 * time.Second
)
