package domain

import "fmt"

// RepositoryError 通用仓储错误
type RepositoryError struct {
	Code     string
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

// 常见仓储错误
var (
	ErrEntityNotFound      = &RepositoryError{Code: "ENTITY_NOT_FOUND", Message: "entity not found"}
	ErrEntityAlreadyExists = &RepositoryError{Code: "ENTITY_ALREADY_EXISTS", Message: "entity already exists"}
	ErrInvalidID           = &RepositoryError{Code: "INVALID_ID", Message: "invalid entity id"}
	ErrVersionConflict     = &RepositoryError{Code: "VERSION_CONFLICT", Message: "version conflict (optimistic lock)"}
	ErrRepositoryFailed    = &RepositoryError{Code: "REPOSITORY_FAILED", Message: "repository operation failed"}
)

// NewNotFoundError 创建未找到错误（便于示例与 mocks 使用）
func NewNotFoundError(format string, args ...any) *RepositoryError {
	return &RepositoryError{
		Code:    "ENTITY_NOT_FOUND",
		Message: fmt.Sprintf(format, args...),
	}
}
