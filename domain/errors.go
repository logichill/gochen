package domain

import "fmt"

// ErrorCode 领域错误码
type ErrorCode string

// 预定义错误码常量（不可变）
const (
	// 仓储操作错误码
	ErrCodeEntityNotFound      ErrorCode = "ENTITY_NOT_FOUND"
	ErrCodeEntityAlreadyExists ErrorCode = "ENTITY_ALREADY_EXISTS"
	ErrCodeInvalidID           ErrorCode = "INVALID_ID"
	ErrCodeVersionConflict     ErrorCode = "VERSION_CONFLICT"
	ErrCodeRepositoryFailed    ErrorCode = "REPOSITORY_FAILED"

	// 实体状态错误码
	ErrCodeAlreadyDeleted ErrorCode = "ALREADY_DELETED"
	ErrCodeNotDeleted     ErrorCode = "NOT_DELETED"
	ErrCodeInvalidVersion ErrorCode = "INVALID_VERSION"
)

// RepositoryError 通用仓储错误
type RepositoryError struct {
	Code     ErrorCode
	Message  string
	EntityID any
	Cause    error
}

func (e *RepositoryError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *RepositoryError) Unwrap() error {
	return e.Cause
}

// Is 实现 errors.Is 接口，基于错误码匹配
func (e *RepositoryError) Is(target error) bool {
	t, ok := target.(*RepositoryError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// 哨兵错误（仅用于 errors.Is 比较，不应直接返回）
var (
	errEntityNotFound      = &RepositoryError{Code: ErrCodeEntityNotFound}
	errEntityAlreadyExists = &RepositoryError{Code: ErrCodeEntityAlreadyExists}
	errInvalidID           = &RepositoryError{Code: ErrCodeInvalidID}
	errVersionConflict     = &RepositoryError{Code: ErrCodeVersionConflict}
	errRepositoryFailed    = &RepositoryError{Code: ErrCodeRepositoryFailed}
)

// ErrEntityNotFound 返回实体未找到错误（用于 errors.Is 比较）
func ErrEntityNotFound() *RepositoryError {
	return errEntityNotFound
}

// ErrEntityAlreadyExists 返回实体已存在错误（用于 errors.Is 比较）
func ErrEntityAlreadyExists() *RepositoryError {
	return errEntityAlreadyExists
}

// ErrInvalidID 返回无效 ID 错误（用于 errors.Is 比较）
func ErrInvalidID() *RepositoryError {
	return errInvalidID
}

// ErrVersionConflict 返回版本冲突错误（用于 errors.Is 比较）
func ErrVersionConflict() *RepositoryError {
	return errVersionConflict
}

// ErrRepositoryFailed 返回仓储操作失败错误（用于 errors.Is 比较）
func ErrRepositoryFailed() *RepositoryError {
	return errRepositoryFailed
}

// NewNotFoundError 创建实体未找到错误
func NewNotFoundError(format string, args ...any) *RepositoryError {
	return &RepositoryError{
		Code:    ErrCodeEntityNotFound,
		Message: fmt.Sprintf(format, args...),
	}
}

// NewNotFoundErrorWithID 创建带实体 ID 的未找到错误
func NewNotFoundErrorWithID(entityID any, entityType string) *RepositoryError {
	return &RepositoryError{
		Code:     ErrCodeEntityNotFound,
		Message:  fmt.Sprintf("%s with id %v not found", entityType, entityID),
		EntityID: entityID,
	}
}

// NewAlreadyExistsError 创建实体已存在错误
func NewAlreadyExistsError(entityID any, entityType string) *RepositoryError {
	return &RepositoryError{
		Code:     ErrCodeEntityAlreadyExists,
		Message:  fmt.Sprintf("%s with id %v already exists", entityType, entityID),
		EntityID: entityID,
	}
}

// NewInvalidIDError 创建无效 ID 错误
func NewInvalidIDError(entityID any, reason string) *RepositoryError {
	return &RepositoryError{
		Code:     ErrCodeInvalidID,
		Message:  reason,
		EntityID: entityID,
	}
}

// NewVersionConflictError 创建版本冲突错误
func NewVersionConflictError(entityID any, expected, actual uint64) *RepositoryError {
	return &RepositoryError{
		Code:     ErrCodeVersionConflict,
		Message:  fmt.Sprintf("version conflict: expected %d, actual %d", expected, actual),
		EntityID: entityID,
	}
}

// NewRepositoryFailedError 创建仓储操作失败错误
func NewRepositoryFailedError(message string, cause error) *RepositoryError {
	return &RepositoryError{
		Code:    ErrCodeRepositoryFailed,
		Message: message,
		Cause:   cause,
	}
}

// ========== 实体状态错误 ==========

// 实体状态错误哨兵（仅用于 errors.Is 比较）
var (
	errAlreadyDeleted = &RepositoryError{Code: ErrCodeAlreadyDeleted}
	errNotDeleted     = &RepositoryError{Code: ErrCodeNotDeleted}
	errInvalidVersion = &RepositoryError{Code: ErrCodeInvalidVersion}
)

// ErrAlreadyDeleted 返回实体已删除错误（用于 errors.Is 比较）
func ErrAlreadyDeleted() *RepositoryError {
	return errAlreadyDeleted
}

// ErrNotDeleted 返回实体未删除错误（用于 errors.Is 比较）
func ErrNotDeleted() *RepositoryError {
	return errNotDeleted
}

// ErrInvalidVersion 返回无效版本错误（用于 errors.Is 比较）
func ErrInvalidVersion() *RepositoryError {
	return errInvalidVersion
}

// NewAlreadyDeletedError 创建实体已删除错误
func NewAlreadyDeletedError(entityID any) *RepositoryError {
	return &RepositoryError{
		Code:     ErrCodeAlreadyDeleted,
		Message:  "entity is already deleted",
		EntityID: entityID,
	}
}

// NewNotDeletedError 创建实体未删除错误
func NewNotDeletedError(entityID any) *RepositoryError {
	return &RepositoryError{
		Code:     ErrCodeNotDeleted,
		Message:  "entity is not deleted",
		EntityID: entityID,
	}
}

// NewInvalidVersionError 创建无效版本错误
func NewInvalidVersionError(entityID any, message string) *RepositoryError {
	return &RepositoryError{
		Code:     ErrCodeInvalidVersion,
		Message:  message,
		EntityID: entityID,
	}
}
