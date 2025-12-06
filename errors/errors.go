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
	// 返回详情的拷贝，避免调用方通过返回 map 修改内部状态
	return copyMap(e.details)
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

// 哨兵错误（仅用于 errors.Is 比较，不捕获堆栈）
// 业务代码应使用 NewXxxError() 工厂函数创建带堆栈的错误实例
var (
	errInternal     = &AppError{code: ErrCodeInternal, message: "内部服务器错误"}
	errInvalidInput = &AppError{code: ErrCodeInvalidInput, message: "无效的输入参数"}
	errNotFound     = &AppError{code: ErrCodeNotFound, message: "资源未找到"}
	errConflict     = &AppError{code: ErrCodeConflict, message: "资源冲突"}
	errUnauthorized = &AppError{code: ErrCodeUnauthorized, message: "未授权访问"}
	errForbidden    = &AppError{code: ErrCodeForbidden, message: "禁止访问"}
	errTimeout      = &AppError{code: ErrCodeTimeout, message: "操作超时"}
	errValidation   = &AppError{code: ErrCodeValidation, message: "数据验证失败"}
	errDuplicate    = &AppError{code: ErrCodeDuplicate, message: "数据重复"}
	errDependency   = &AppError{code: ErrCodeDependency, message: "依赖错误"}
	errConcurrency  = &AppError{code: ErrCodeConcurrency, message: "并发冲突"}
	errDatabase     = &AppError{code: ErrCodeDatabase, message: "数据库错误"}
	errCache        = &AppError{code: ErrCodeCache, message: "缓存错误"}
	errQueue        = &AppError{code: ErrCodeQueue, message: "队列错误"}
	errNetwork      = &AppError{code: ErrCodeNetwork, message: "网络错误"}
)

// ========== 哨兵错误访问函数（用于 errors.Is 比较）==========

// ErrInternal 返回内部错误哨兵（用于 errors.Is 比较）
func ErrInternal() *AppError { return errInternal }

// ErrInvalidInput 返回无效输入错误哨兵（用于 errors.Is 比较）
func ErrInvalidInput() *AppError { return errInvalidInput }

// ErrNotFound 返回未找到错误哨兵（用于 errors.Is 比较）
func ErrNotFound() *AppError { return errNotFound }

// ErrConflict 返回冲突错误哨兵（用于 errors.Is 比较）
func ErrConflict() *AppError { return errConflict }

// ErrUnauthorized 返回未授权错误哨兵（用于 errors.Is 比较）
func ErrUnauthorized() *AppError { return errUnauthorized }

// ErrForbidden 返回禁止访问错误哨兵（用于 errors.Is 比较）
func ErrForbidden() *AppError { return errForbidden }

// ErrTimeout 返回超时错误哨兵（用于 errors.Is 比较）
func ErrTimeout() *AppError { return errTimeout }

// ErrValidation 返回验证错误哨兵（用于 errors.Is 比较）
func ErrValidation() *AppError { return errValidation }

// ErrDuplicate 返回重复错误哨兵（用于 errors.Is 比较）
func ErrDuplicate() *AppError { return errDuplicate }

// ErrDependency 返回依赖错误哨兵（用于 errors.Is 比较）
func ErrDependency() *AppError { return errDependency }

// ErrConcurrency 返回并发错误哨兵（用于 errors.Is 比较）
func ErrConcurrency() *AppError { return errConcurrency }

// ErrDatabase 返回数据库错误哨兵（用于 errors.Is 比较）
func ErrDatabase() *AppError { return errDatabase }

// ErrCache 返回缓存错误哨兵（用于 errors.Is 比较）
func ErrCache() *AppError { return errCache }

// ErrQueue 返回队列错误哨兵（用于 errors.Is 比较）
func ErrQueue() *AppError { return errQueue }

// ErrNetwork 返回网络错误哨兵（用于 errors.Is 比较）
func ErrNetwork() *AppError { return errNetwork }

// ========== 工厂函数（创建带堆栈的错误实例）==========

// NewInternalError 创建内部错误
func NewInternalError(message string) IError {
	return NewError(ErrCodeInternal, message)
}

// NewInvalidInputError 创建无效输入错误
func NewInvalidInputError(message string) IError {
	return NewError(ErrCodeInvalidInput, message)
}

// NewNotFoundError 创建未找到错误
func NewNotFoundError(message string) IError {
	return NewError(ErrCodeNotFound, message)
}

// NewConflictError 创建冲突错误
func NewConflictError(message string) IError {
	return NewError(ErrCodeConflict, message)
}

// NewUnauthorizedError 创建未授权错误
func NewUnauthorizedError(message string) IError {
	return NewError(ErrCodeUnauthorized, message)
}

// NewForbiddenError 创建禁止访问错误
func NewForbiddenError(message string) IError {
	return NewError(ErrCodeForbidden, message)
}

// NewTimeoutError 创建超时错误
func NewTimeoutError(message string) IError {
	return NewError(ErrCodeTimeout, message)
}

// NewValidationError 创建验证错误
func NewValidationError(message string) IError {
	return NewError(ErrCodeValidation, message)
}

// NewDuplicateError 创建重复错误
func NewDuplicateError(message string) IError {
	return NewError(ErrCodeDuplicate, message)
}

// NewDependencyError 创建依赖错误
func NewDependencyError(message string) IError {
	return NewError(ErrCodeDependency, message)
}

// NewConcurrencyError 创建并发错误
func NewConcurrencyError(message string) IError {
	return NewError(ErrCodeConcurrency, message)
}

// NewDatabaseError 创建数据库错误
func NewDatabaseError(message string, cause error) IError {
	return NewErrorWithCause(ErrCodeDatabase, message, cause)
}

// NewCacheError 创建缓存错误
func NewCacheError(message string, cause error) IError {
	return NewErrorWithCause(ErrCodeCache, message, cause)
}

// NewQueueError 创建队列错误
func NewQueueError(message string, cause error) IError {
	return NewErrorWithCause(ErrCodeQueue, message, cause)
}

// NewNetworkError 创建网络错误
func NewNetworkError(message string, cause error) IError {
	return NewErrorWithCause(ErrCodeNetwork, message, cause)
}

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
