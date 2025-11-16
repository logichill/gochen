package httpx

import "time"

// ListRequest 表示分页与排序请求参数
type ListRequest struct {
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Sort     map[string]string `json:"sort"`
}

// NewListRequest 创建分页请求对象
func NewListRequest(page, pageSize int) *ListRequest {
	return &ListRequest{
		Page:     page,
		PageSize: pageSize,
		Sort:     make(map[string]string),
	}
}

// SetSort 设置排序字段
func (r *ListRequest) SetSort(field, direction string) {
	if r.Sort == nil {
		r.Sort = make(map[string]string)
	}
	r.Sort[field] = direction
}

// ErrorPayload 通用错误响应
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// NewErrorResponse 创建错误响应
func NewErrorResponse(code, message, details string) *ErrorPayload {
	return &ErrorPayload{
		Code:    code,
		Message: message,
		Details: details,
	}
}

// SuccessPayload 通用成功响应
type SuccessPayload struct {
	Data interface{} `json:"data"`
}

// NewSuccessResponse 创建成功响应
func NewSuccessResponse(data interface{}) *SuccessPayload {
	return &SuccessPayload{
		Data: data,
	}
}

// WebConfig HTTP 服务基础配置
type WebConfig struct {
	Host         string        `json:"host" yaml:"host"`
	Port         int           `json:"port" yaml:"port"`
	Mode         string        `json:"mode" yaml:"mode"`
	ReadTimeout  time.Duration `json:"read_timeout" yaml:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout" yaml:"write_timeout"`
	IdleTimeout  time.Duration `json:"idle_timeout" yaml:"idle_timeout"`

	// TLS
	TLSEnabled bool   `json:"tls_enabled" yaml:"tls_enabled"`
	CertFile   string `json:"cert_file" yaml:"cert_file"`
	KeyFile    string `json:"key_file" yaml:"key_file"`

	// CORS
	CORSEnabled      bool     `json:"cors_enabled" yaml:"cors_enabled"`
	CORSAllowOrigins []string `json:"cors_allow_origins" yaml:"cors_allow_origins"`
	CORSAllowMethods []string `json:"cors_allow_methods" yaml:"cors_allow_methods"`
	CORSAllowHeaders []string `json:"cors_allow_headers" yaml:"cors_allow_headers"`
}
