package errors

// PublicMessage 返回错误码对应的对外安全文案。
//
// 未识别的错误码返回空字符串，由调用方决定兜底文案，避免不同子系统被强制统一话术。
func PublicMessage(code ErrorCode) string {
	switch code {
	case InvalidInput:
		return "invalid input"
	case PayloadTooLarge:
		return "payload too large"
	case NotFound:
		return "not found"
	case Conflict:
		return "conflict"
	case FailedPrecondition:
		return "failed precondition"
	case Unauthorized:
		return "unauthorized"
	case Forbidden:
		return "forbidden"
	case Timeout:
		return "timeout"
	case TooManyRequests:
		return "too many requests"
	case ServiceUnavailable:
		return "service unavailable"
	case Unsupported:
		return "unsupported operation"
	case Validation:
		return "validation failed"
	case Duplicate:
		return "duplicate"
	default:
		return ""
	}
}
