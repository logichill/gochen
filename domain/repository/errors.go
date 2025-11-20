package repository

// 常见错误
var (
	ErrEntityNotFound      = &RepositoryError{Code: "ENTITY_NOT_FOUND", Message: "entity not found"}
	ErrEntityAlreadyExists = &RepositoryError{Code: "ENTITY_ALREADY_EXISTS", Message: "entity already exists"}
	ErrInvalidID           = &RepositoryError{Code: "INVALID_ID", Message: "invalid entity id"}
	ErrVersionConflict     = &RepositoryError{Code: "VERSION_CONFLICT", Message: "version conflict (optimistic lock)"}
	ErrRepositoryFailed    = &RepositoryError{Code: "REPOSITORY_FAILED", Message: "repository operation failed"}
)

// RepositoryError 仓储错误
type RepositoryError struct {
	Code     string
	Message  string
	EntityID interface{}
	Cause    error
}

func (e *RepositoryError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *RepositoryError) Unwrap() error {
	return e.Cause
}
