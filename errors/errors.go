package errors

import (
	stdErrors "errors"
	"fmt"
	"runtime"
	"strings"
)

// ErrorCode 错误代码类型
type ErrorCode string

// 预定义错误代码
const (
	// 通用错误代码
	ErrCodeInternal           ErrorCode = "INTERNAL_ERROR"
	ErrCodeInvalidInput       ErrorCode = "INVALID_INPUT"
	ErrCodeNotFound           ErrorCode = "NOT_FOUND"
	ErrCodeConflict           ErrorCode = "CONFLICT"
	ErrCodeUnauthorized       ErrorCode = "UNAUTHORIZED"
	ErrCodeForbidden          ErrorCode = "FORBIDDEN"
	ErrCodeTimeout            ErrorCode = "TIMEOUT"
	ErrCodeTooManyRequests    ErrorCode = "TOO_MANY_REQUESTS"
	ErrCodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"

	// 业务错误代码
	ErrCodeValidation  ErrorCode = "VALIDATION_ERROR"
	ErrCodeDuplicate   ErrorCode = "DUPLICATE_ERROR"
	ErrCodeDependency  ErrorCode = "DEPENDENCY_ERROR"
	ErrCodeConcurrency ErrorCode = "CONCURRENCY_ERROR"

	// 基础设施错误代码
	ErrCodeDatabase ErrorCode = "DATABASE_ERROR"
	ErrCodeCache    ErrorCode = "CACHE_ERROR"
	ErrCodeQueue    ErrorCode = "QUEUE_ERROR"
	ErrCodeNetwork  ErrorCode = "NETWORK_ERROR"
)

// IError 错误接口
type IError interface {
	error

	// 获取错误代码
	Code() ErrorCode

	// 获取错误消息
	Message() string

	// 获取原始错误
	Cause() error

	// 获取错误详情
	Details() map[string]any

	// 获取堆栈信息
	Stack() string

	// 是否为指定类型的错误
	Is(target error) bool

	// 包装错误
	Wrap(msg string) IError

	// 添加详情
	WithDetails(details map[string]any) IError

	// 添加上下文
	WithContext(key string, value any) IError
}

// AppError 应用错误实现
type AppError struct {
	code    ErrorCode
	message string
	cause   error
	details map[string]any
	stack   string
}

// NewError 创建新错误
func NewError(code ErrorCode, message string) IError {
	return &AppError{
		code:    code,
		message: message,
		details: make(map[string]any),
		stack:   captureStack(),
	}
}

// NewErrorWithCause 创建带原因的错误
func NewErrorWithCause(code ErrorCode, message string, cause error) IError {
	return &AppError{
		code:    code,
		message: message,
		cause:   cause,
		details: make(map[string]any),
		stack:   captureStack(),
	}
}

// WrapError 包装错误
func WrapError(err error, code ErrorCode, message string) IError {
	if err == nil {
		return nil
	}

	return &AppError{
		code:    code,
		message: message,
		cause:   err,
		details: make(map[string]any),
		stack:   captureStack(),
	}
}

// Error 实现 error 接口
func (e *AppError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.code, e.message, e.cause)
	}
	return fmt.Sprintf("[%s] %s", e.code, e.message)
}

// Code 获取错误代码
func (e *AppError) Code() ErrorCode {
	return e.code
}

// Message 获取错误消息
func (e *AppError) Message() string {
	return e.message
}

// Cause 获取原始错误
func (e *AppError) Cause() error {
	return e.cause
}

// Details 获取错误详情
func (e *AppError) Details() map[string]any {
	if e.details == nil {
		e.details = make(map[string]any)
	}
	return e.details
}

// Stack 获取堆栈信息
func (e *AppError) Stack() string {
	return e.stack
}

// Is 检查是否为指定类型的错误
func (e *AppError) Is(target error) bool {
	if target == nil {
		return false
	}

	if appErr, ok := target.(*AppError); ok {
		return e.code == appErr.code
	}

	if e.cause != nil {
		return stdErrors.Is(e.cause, target)
	}

	return false
}

// Unwrap 解包错误（支持 errors.Unwrap）
func (e *AppError) Unwrap() error {
	return e.cause
}

// Wrap 包装错误
func (e *AppError) Wrap(msg string) IError {
	return &AppError{
		code:    e.code,
		message: fmt.Sprintf("%s: %s", msg, e.message),
		cause:   e,
		details: copyMap(e.details),
		stack:   captureStack(),
	}
}

// WithDetails 添加详情
func (e *AppError) WithDetails(details map[string]any) IError {
	newDetails := copyMap(e.details)
	for k, v := range details {
		newDetails[k] = v
	}

	return &AppError{
		code:    e.code,
		message: e.message,
		cause:   e.cause,
		details: newDetails,
		stack:   e.stack,
	}
}

// WithContext 添加上下文
func (e *AppError) WithContext(key string, value any) IError {
	newDetails := copyMap(e.details)
	newDetails[key] = value

	return &AppError{
		code:    e.code,
		message: e.message,
		cause:   e.cause,
		details: newDetails,
		stack:   e.stack,
	}
}

// 预定义错误变量
var (
	ErrInternal     = NewError(ErrCodeInternal, "内部服务器错误")
	ErrInvalidInput = NewError(ErrCodeInvalidInput, "无效的输入参数")
	ErrNotFound     = NewError(ErrCodeNotFound, "资源未找到")
	ErrConflict     = NewError(ErrCodeConflict, "资源冲突")
	ErrUnauthorized = NewError(ErrCodeUnauthorized, "未授权访问")
	ErrForbidden    = NewError(ErrCodeForbidden, "禁止访问")
	ErrTimeout      = NewError(ErrCodeTimeout, "操作超时")
	ErrValidation   = NewError(ErrCodeValidation, "数据验证失败")
	ErrDuplicate    = NewError(ErrCodeDuplicate, "数据重复")
	ErrDependency   = NewError(ErrCodeDependency, "依赖错误")
	ErrConcurrency  = NewError(ErrCodeConcurrency, "并发冲突")
	ErrDatabase     = NewError(ErrCodeDatabase, "数据库错误")
	ErrCache        = NewError(ErrCodeCache, "缓存错误")
	ErrQueue        = NewError(ErrCodeQueue, "队列错误")
	ErrNetwork      = NewError(ErrCodeNetwork, "网络错误")
)

// IsNotFound 检查是否为未找到错误
func IsNotFound(err error) bool {
	return IsErrorCode(err, ErrCodeNotFound)
}

// IsValidation 检查是否为验证错误
func IsValidation(err error) bool {
	return IsErrorCode(err, ErrCodeValidation)
}

// IsConflict 检查是否为冲突错误
func IsConflict(err error) bool {
	return IsErrorCode(err, ErrCodeConflict)
}

// IsErrorCode 检查是否为指定错误代码
func IsErrorCode(err error, code ErrorCode) bool {
	if err == nil {
		return false
	}

	var appErr *AppError
	if stdErrors.As(err, &appErr) {
		return appErr.code == code
	}

	return false
}

// GetErrorCode 获取错误代码
func GetErrorCode(err error) ErrorCode {
	if err == nil {
		return ""
	}

	var appErr *AppError
	if stdErrors.As(err, &appErr) {
		return appErr.code
	}

	return ErrCodeInternal
}

// captureStack 捕获堆栈信息
func captureStack() string {
	const depth = 32
	var pcs [depth]uintptr
	n := runtime.Callers(3, pcs[:])

	var builder strings.Builder
	frames := runtime.CallersFrames(pcs[:n])

	for {
		frame, more := frames.Next()
		builder.WriteString(fmt.Sprintf("%s:%d %s\n", frame.File, frame.Line, frame.Function))

		if !more {
			break
		}
	}

	return builder.String()
}

// copyMap 复制映射
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
