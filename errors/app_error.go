package errors

import (
	"fmt"
)

// AppError 表示带错误码、原因与上下文的应用错误。
type AppError struct {
	code    ErrorCode
	message string
	cause   error
	details map[string]any
}

// IAppError 是应用错误的稳定构造结果。
// 构造函数返回该接口，使 Wrap(nil, ...) 能返回真正的 nil interface，同时保留 fluent API。
type IAppError interface {
	error
	Code() ErrorCode
	Message() string
	Details() map[string]any
	Unwrap() error
	Wrap(msg string) IAppError
	WithDetails(details map[string]any) IAppError
	WithContext(key string, value any) IAppError
}

var _ IAppError = (*AppError)(nil)

// Error 实现 error 接口，并把错误码、消息与 cause 组合成可读文本。
func (e *AppError) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.cause != nil {
		// 避免 message 与 cause 文本相同时出现重复输出（例如 Normalize 直接用 err.Error() 作为 message）
		if e.message == "" || e.message == e.cause.Error() {
			return fmt.Sprintf("[%s] %s", e.code, e.cause.Error())
		}
		return fmt.Sprintf("[%s] %s: %v", e.code, e.message, e.cause)
	}
	return fmt.Sprintf("[%s] %s", e.code, e.message)
}

// Is 允许 errors.Is(err, ErrorCode) 直接按错误码匹配。
func (e *AppError) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	switch typed := target.(type) {
	case ErrorCode:
		return e.code == typed
	case *AppError:
		return typed != nil && typed.code != "" && e.code == typed.code
	}
	if target == ErrUnsupported {
		return e.code == Unsupported
	}
	if coder, ok := target.(IErrorCoder); ok {
		return e.code == coder.ErrorCode()
	}
	return false
}

func (e *AppError) Code() ErrorCode {
	return e.code
}

func (e *AppError) Message() string {
	return e.message
}

func (e *AppError) Details() map[string]any {
	if e == nil {
		return nil
	}
	// 返回详情的拷贝，避免调用方通过返回 map 修改内部状态
	return copyMap(e.details)
}

// Unwrap 返回底层 cause，兼容标准库错误链。
func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Wrap 基于当前错误再包一层消息，保留原错误链和详情。
func (e *AppError) Wrap(msg string) IAppError {
	return &AppError{
		code:    e.code,
		message: fmt.Sprintf("%s: %s", msg, e.message),
		cause:   e,
		details: copyMap(e.details),
	}
}

// WithDetails 合并一组详情字段并返回新错误对象。
func (e *AppError) WithDetails(details map[string]any) IAppError {
	newDetails := copyMap(e.details)
	for k, v := range details {
		newDetails[k] = v
	}

	return &AppError{
		code:    e.code,
		message: e.message,
		cause:   e.cause,
		details: newDetails,
	}
}

// WithContext 追加单个上下文字段并返回新错误对象。
func (e *AppError) WithContext(key string, value any) IAppError {
	newDetails := copyMap(e.details)
	newDetails[key] = value

	return &AppError{
		code:    e.code,
		message: e.message,
		cause:   e.cause,
		details: newDetails,
	}
}

// copyMap 复制映射。
func copyMap(original map[string]any) map[string]any {
	if original == nil {
		return make(map[string]any)
	}

	copied := make(map[string]any, len(original))
	for k, v := range original {
		copied[k] = v
	}

	return copied
}
