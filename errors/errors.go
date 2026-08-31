package errors

import (
	stderrors "errors"
)

// ErrUnsupported 等同于标准库 errors.ErrUnsupported。
var ErrUnsupported = stderrors.ErrUnsupported

// New 返回与标准库 errors.New 一致的基础错误。
func New(message string) error {
	return stderrors.New(message)
}

// NewCode 创建带错误码的应用错误。
func NewCode(code ErrorCode, message string) IAppError {
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
func NewCodeWithCause(code ErrorCode, message string, cause error) IAppError {
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
func Wrap(cause error, code ErrorCode, message string) IAppError {
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
