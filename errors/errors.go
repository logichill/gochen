package errors

import (
	stderrors "errors"
	"fmt"
	"runtime"
	"strings"
)

// ErrUnsupported 等同于标准库 errors.ErrUnsupported。
var ErrUnsupported = stderrors.ErrUnsupported

// AppError 表示带错误码、原因与上下文的应用错误。
type AppError struct {
	code    ErrorCode
	message string
	cause   error
	details map[string]any
}

// New 返回与标准库 errors.New 一致的基础错误。
func New(message string) error {
	return stderrors.New(message)
}

// NewCode 创建带错误码的应用错误。
func NewCode(code ErrorCode, message string) *AppError {
	details := make(map[string]any)
	if shouldCaptureStack(code) {
		details["stack"] = captureStack(3)
	}
	return &AppError{
		code:    code,
		message: message,
		details: details,
	}
}

// NewCodeWithCause 创建带 cause 的应用错误。
func NewCodeWithCause(code ErrorCode, message string, cause error) *AppError {
	details := make(map[string]any)
	if shouldCaptureStack(code) {
		if stack, ok := stackFromCause(cause); ok {
			details["stack"] = stack
		} else {
			details["stack"] = captureStack(3)
		}
	}
	return &AppError{
		code:    code,
		message: message,
		cause:   cause,
		details: details,
	}
}

// Wrap 把底层错误包装成带错误码和上下文的应用错误。
func Wrap(cause error, code ErrorCode, message string) *AppError {
	if cause == nil {
		return nil
	}

	details := make(map[string]any)
	if shouldCaptureStack(code) {
		if stack, ok := stackFromCause(cause); ok {
			details["stack"] = stack
		} else {
			details["stack"] = captureStack(3)
		}
	}
	return &AppError{
		code:    code,
		message: message,
		cause:   cause,
		details: details,
	}
}

// Is 返回与标准库 errors.Is 一致的匹配结果。
func Is(err, target error) bool { return stderrors.Is(err, target) }

// As 返回与标准库 errors.As 一致的匹配结果。
func As(err error, target any) bool { return stderrors.As(err, target) }

// AsType 返回与标准库 errors.AsType 一致的匹配结果。
func AsType[E error](err error) (E, bool) { return stderrors.AsType[E](err) }

// Join 返回与标准库 errors.Join 一致的聚合错误。
func Join(errs ...error) error { return stderrors.Join(errs...) }

// Unwrap 返回与标准库 errors.Unwrap 一致的解包结果。
func Unwrap(err error) error { return stderrors.Unwrap(err) }

// Code 提取 err 的错误码。
//
// 约定：
// - err 为 nil 时返回 ""；
// - 未识别的错误返回 Internal（便于 HTTP 映射）。
func Code(err error) ErrorCode {
	if err == nil {
		return ""
	}
	// 接口值里直接装箱的 typed-nil *AppError 需要视为 nil；
	// 否则既不会被链路扫描命中，也不会触发 As(..., *AppError)，最终会错误落到 Internal。
	if appErr, ok := err.(*AppError); ok && appErr == nil {
		return ""
	}

	if appErr, ok := findAppError(err); ok {
		return appErr.code
	}

	var coder IErrorCoder
	if As(err, &coder) && coder != nil {
		return coder.ErrorCode()
	}

	if Is(err, ErrUnsupported) {
		return Unsupported
	}

	return Internal
}

func findAppError(err error) (*AppError, bool) {
	if err == nil {
		return nil, false
	}
	if appErr, ok := err.(*AppError); ok {
		if appErr == nil {
			return nil, false
		}
		return appErr, true
	}
	// 自定义 As(target) 可能直接暴露 AppError；先查当前节点，避免多错误场景被前序 typed-nil 干扰。
	if aser, ok := err.(interface{ As(any) bool }); ok {
		var appErr *AppError
		if aser.As(&appErr) && appErr != nil {
			return appErr, true
		}
	}
	if unwrapper, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range unwrapper.Unwrap() {
			if appErr, found := findAppError(child); found {
				return appErr, true
			}
		}
		return nil, false
	}
	if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
		return findAppError(unwrapper.Unwrap())
	}
	return nil, false
}

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
func (e *AppError) Wrap(msg string) *AppError {
	return &AppError{
		code:    e.code,
		message: fmt.Sprintf("%s: %s", msg, e.message),
		cause:   e,
		details: copyMap(e.details),
	}
}

// WithDetails 合并一组详情字段并返回新错误对象。
func (e *AppError) WithDetails(details map[string]any) *AppError {
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
func (e *AppError) WithContext(key string, value any) *AppError {
	newDetails := copyMap(e.details)
	newDetails[key] = value

	return &AppError{
		code:    e.code,
		message: e.message,
		cause:   e.cause,
		details: newDetails,
	}
}

// shouldCaptureStack 判断该错误码是否应自动附带堆栈信息。
func shouldCaptureStack(code ErrorCode) bool {
	return ErrorCodeToHTTPStatus(code) >= 500
}

// stackFromCause 尝试从底层错误链中提取已有堆栈。
func stackFromCause(cause error) (string, bool) {
	if cause == nil {
		return "", false
	}
	var appErr *AppError
	if !As(cause, &appErr) || appErr == nil {
		return "", false
	}
	stack, ok := appErr.details["stack"].(string)
	if !ok || strings.TrimSpace(stack) == "" {
		return "", false
	}
	return stack, true
}

func captureStack(skip int) string {
	const maxDepth = 32
	pcs := make([]uintptr, maxDepth)
	n := runtime.Callers(skip, pcs)
	pcs = pcs[:n]

	frames := runtime.CallersFrames(pcs)
	var sb strings.Builder
	for {
		frame, more := frames.Next()
		if frame.Function != "" {
			sb.WriteString(frame.Function)
		} else {
			sb.WriteString("<unknown>")
		}
		sb.WriteByte('\n')
		fmt.Fprintf(&sb, "\t%s:%d\n", frame.File, frame.Line)
		if !more {
			break
		}
	}
	return strings.TrimRight(sb.String(), "\n")
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
