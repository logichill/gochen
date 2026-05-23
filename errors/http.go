package errors

// ========== HTTP 状态码映射 ==========

// ToHTTPStatus 将错误映射到 HTTP 状态码。
//
// 说明：
// - 这是一个便捷函数，供 HTTP 层快速获取状态码。
// - 对于更复杂的错误响应处理，建议使用 gochen/api/rest 包的 rest.DefaultErrorHandler。
func ToHTTPStatus(err error) int {
	code := Code(err)
	if code == "" {
		return 200
	}
	return ErrorCodeToHTTPStatus(code)
}

// ErrorCodeToHTTPStatus 将错误码映射到 HTTP 状态码。
func ErrorCodeToHTTPStatus(code ErrorCode) int {
	switch code {
	case NotFound:
		return 404
	case InvalidInput, Validation:
		return 400
	case PayloadTooLarge:
		return 413
	case Conflict, Duplicate, Concurrency:
		return 409
	case Unauthorized:
		return 401
	case Forbidden:
		return 403
	case Timeout:
		return 408
	case TooManyRequests:
		return 429
	case Unsupported:
		return 400
	case ServiceUnavailable:
		return 503
	default:
		return 500
	}
}
