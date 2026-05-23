package errors

// ErrorCode 错误代码类型。
type ErrorCode string

// Error 让 ErrorCode 可作为 errors.Is 的匹配目标使用。
func (code ErrorCode) Error() string { return string(code) }

// ErrorCode 返回当前错误码本身，便于统一走 IErrorCoder 约定。
func (code ErrorCode) ErrorCode() ErrorCode { return code }

// 预定义错误代码。
const (
	// Internal 表示未分类的服务端内部错误。
	Internal ErrorCode = "INTERNAL_ERROR"
	// InvalidInput 表示请求参数、配置或调用输入不合法。
	InvalidInput ErrorCode = "INVALID_INPUT"
	// PayloadTooLarge 表示请求体或消息载荷超出限制。
	PayloadTooLarge ErrorCode = "PAYLOAD_TOO_LARGE"
	// NotFound 表示目标资源不存在。
	NotFound ErrorCode = "NOT_FOUND"
	// Conflict 表示状态冲突或版本冲突。
	Conflict ErrorCode = "CONFLICT"
	// Unauthorized 表示调用方未认证。
	Unauthorized ErrorCode = "UNAUTHORIZED"
	// Forbidden 表示调用方已认证但无权限。
	Forbidden ErrorCode = "FORBIDDEN"
	// Timeout 表示操作超时。
	Timeout ErrorCode = "TIMEOUT"
	// TooManyRequests 表示触发限流。
	TooManyRequests ErrorCode = "TOO_MANY_REQUESTS"
	// ServiceUnavailable 表示依赖服务或当前服务暂不可用。
	ServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"
	// Unsupported 表示调用方请求了当前不支持的能力。
	Unsupported ErrorCode = "UNSUPPORTED_OPERATION"

	// Validation 表示业务校验失败。
	Validation ErrorCode = "VALIDATION_ERROR"
	// Duplicate 表示唯一性或重复提交冲突。
	Duplicate ErrorCode = "DUPLICATE_ERROR"
	// Dependency 表示依赖组件、装配或外部服务失败。
	Dependency ErrorCode = "DEPENDENCY_ERROR"
	// Concurrency 表示并发写入或乐观锁冲突。
	Concurrency ErrorCode = "CONCURRENCY_ERROR"

	// Database 表示数据库访问失败。
	Database ErrorCode = "DATABASE_ERROR"
	// Cache 表示缓存访问失败。
	Cache ErrorCode = "CACHE_ERROR"
	// Queue 表示队列或消息基础设施失败。
	Queue ErrorCode = "QUEUE_ERROR"
	// Network 表示网络访问失败。
	Network ErrorCode = "NETWORK_ERROR"
)
