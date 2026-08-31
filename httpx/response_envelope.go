package httpx

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
