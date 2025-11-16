// Package service 提供服务层错误定义
package service

import (
	"errors"
	"fmt"
)

// ServiceError 服务层错误类型
type ServiceError struct {
	Code    string
	Message string
	Cause   error
}

func (e *ServiceError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *ServiceError) Unwrap() error {
	return e.Cause
}

// NewValidationError 创建验证错误
func NewValidationError(format string, args ...interface{}) *ServiceError {
	return &ServiceError{
		Code:    "VALIDATION_ERROR",
		Message: fmt.Sprintf(format, args...),
	}
}

// NewNotFoundError 创建未找到错误
func NewNotFoundError(format string, args ...interface{}) *ServiceError {
	return &ServiceError{
		Code:    "NOT_FOUND",
		Message: fmt.Sprintf(format, args...),
	}
}

// NewConflictError 创建冲突错误
func NewConflictError(format string, args ...interface{}) *ServiceError {
	return &ServiceError{
		Code:    "CONFLICT",
		Message: fmt.Sprintf(format, args...),
	}
}

// NewUnauthorizedError 创建未授权错误
func NewUnauthorizedError(format string, args ...interface{}) *ServiceError {
	return &ServiceError{
		Code:    "UNAUTHORIZED",
		Message: fmt.Sprintf(format, args...),
	}
}

// NewForbiddenError 创建禁止访问错误
func NewForbiddenError(format string, args ...interface{}) *ServiceError {
	return &ServiceError{
		Code:    "FORBIDDEN",
		Message: fmt.Sprintf(format, args...),
	}
}

// NewTooManyRequestsError 创建请求过多错误
func NewTooManyRequestsError(format string, args ...interface{}) *ServiceError {
	return &ServiceError{
		Code:    "TOO_MANY_REQUESTS",
		Message: fmt.Sprintf(format, args...),
	}
}

// NewInternalError 创建内部错误
func NewInternalError(cause error, format string, args ...interface{}) *ServiceError {
	return &ServiceError{
		Code:    "INTERNAL_ERROR",
		Message: fmt.Sprintf(format, args...),
		Cause:   cause,
	}
}

// HandleRepositoryError 处理仓储层错误
func HandleRepositoryError(err error) error {
	if err == nil {
		return nil
	}

	// 这里可以根据具体的仓储错误类型进行转换
	// 暂时返回内部错误
	return NewInternalError(err, "repository operation failed")
}

// IsValidationError 检查是否为验证错误
func IsValidationError(err error) bool {
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.Code == "VALIDATION_ERROR"
	}
	return false
}

// IsNotFoundError 检查是否为未找到错误
func IsNotFoundError(err error) bool {
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.Code == "NOT_FOUND"
	}
	return false
}

// IsConflictError 检查是否为冲突错误
func IsConflictError(err error) bool {
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.Code == "CONFLICT"
	}
	return false
}

// IsUnauthorizedError 检查是否为未授权错误
func IsUnauthorizedError(err error) bool {
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.Code == "UNAUTHORIZED"
	}
	return false
}

// IsForbiddenError 检查是否为禁止访问错误
func IsForbiddenError(err error) bool {
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.Code == "FORBIDDEN"
	}
	return false
}

// IsTooManyRequestsError 检查是否为请求过多错误
func IsTooManyRequestsError(err error) bool {
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.Code == "TOO_MANY_REQUESTS"
	}
	return false
}

// IsInternalError 检查是否为内部错误
func IsInternalError(err error) bool {
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.Code == "INTERNAL_ERROR"
	}
	return false
}
