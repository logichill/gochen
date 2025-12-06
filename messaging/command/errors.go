package command

import "fmt"

// ErrorCode 命令错误码
type ErrorCode string

// 预定义错误码常量（不可变）
const (
	ErrCodeInvalidCommand              ErrorCode = "INVALID_COMMAND"
	ErrCodeCommandHandlerNotFound      ErrorCode = "COMMAND_HANDLER_NOT_FOUND"
	ErrCodeCommandHandlerAlreadyExists ErrorCode = "COMMAND_HANDLER_ALREADY_REGISTERED"
	ErrCodeInvalidCommandType          ErrorCode = "INVALID_COMMAND_TYPE"
	ErrCodeCommandExecutionFailed      ErrorCode = "COMMAND_EXECUTION_FAILED"
	ErrCodeAggregateNotFound           ErrorCode = "AGGREGATE_NOT_FOUND"
	ErrCodeConcurrencyConflict         ErrorCode = "CONCURRENCY_CONFLICT"
)

// CommandError 命令错误
type CommandError struct {
	Code        ErrorCode
	Message     string
	CommandType string
	Cause       error
}

func (e *CommandError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (cause: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *CommandError) Unwrap() error { return e.Cause }

// Is 实现 errors.Is 接口，基于错误码匹配
func (e *CommandError) Is(target error) bool {
	t, ok := target.(*CommandError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// 哨兵错误（仅用于 errors.Is 比较，不应直接返回）
var (
	errInvalidCommand              = &CommandError{Code: ErrCodeInvalidCommand}
	errCommandHandlerNotFound      = &CommandError{Code: ErrCodeCommandHandlerNotFound}
	errCommandHandlerAlreadyExists = &CommandError{Code: ErrCodeCommandHandlerAlreadyExists}
	errInvalidCommandType          = &CommandError{Code: ErrCodeInvalidCommandType}
	errCommandExecutionFailed      = &CommandError{Code: ErrCodeCommandExecutionFailed}
	errAggregateNotFound           = &CommandError{Code: ErrCodeAggregateNotFound}
	errConcurrencyConflict         = &CommandError{Code: ErrCodeConcurrencyConflict}
)

// ErrInvalidCommand 返回无效命令错误（用于 errors.Is 比较）
func ErrInvalidCommand() *CommandError {
	return errInvalidCommand
}

// ErrCommandHandlerNotFound 返回命令处理器未找到错误（用于 errors.Is 比较）
func ErrCommandHandlerNotFound() *CommandError {
	return errCommandHandlerNotFound
}

// ErrCommandHandlerAlreadyRegistered 返回命令处理器已注册错误（用于 errors.Is 比较）
func ErrCommandHandlerAlreadyRegistered() *CommandError {
	return errCommandHandlerAlreadyExists
}

// ErrInvalidCommandType 返回无效命令类型错误（用于 errors.Is 比较）
func ErrInvalidCommandType() *CommandError {
	return errInvalidCommandType
}

// ErrCommandExecutionFailed 返回命令执行失败错误（用于 errors.Is 比较）
func ErrCommandExecutionFailed() *CommandError {
	return errCommandExecutionFailed
}

// ErrAggregateNotFound 返回聚合未找到错误（用于 errors.Is 比较）
func ErrAggregateNotFound() *CommandError {
	return errAggregateNotFound
}

// ErrConcurrencyConflict 返回并发冲突错误（用于 errors.Is 比较）
func ErrConcurrencyConflict() *CommandError {
	return errConcurrencyConflict
}

// NewInvalidCommandError 创建无效命令错误
func NewInvalidCommandError(commandType, reason string) *CommandError {
	return &CommandError{
		Code:        ErrCodeInvalidCommand,
		Message:     reason,
		CommandType: commandType,
	}
}

// NewCommandHandlerNotFoundError 创建命令处理器未找到错误
func NewCommandHandlerNotFoundError(commandType string) *CommandError {
	return &CommandError{
		Code:        ErrCodeCommandHandlerNotFound,
		Message:     fmt.Sprintf("no handler registered for command type: %s", commandType),
		CommandType: commandType,
	}
}

// NewCommandExecutionFailedError 创建命令执行失败错误
func NewCommandExecutionFailedError(commandType string, cause error) *CommandError {
	return &CommandError{
		Code:        ErrCodeCommandExecutionFailed,
		Message:     "command execution failed",
		CommandType: commandType,
		Cause:       cause,
	}
}

// NewAggregateNotFoundError 创建聚合未找到错误
func NewAggregateNotFoundError(aggregateID int64) *CommandError {
	return &CommandError{
		Code:    ErrCodeAggregateNotFound,
		Message: fmt.Sprintf("aggregate %d not found", aggregateID),
	}
}

// NewConcurrencyConflictError 创建并发冲突错误
func NewConcurrencyConflictError(aggregateID int64, expected, actual uint64) *CommandError {
	return &CommandError{
		Code:    ErrCodeConcurrencyConflict,
		Message: fmt.Sprintf("concurrency conflict on aggregate %d: expected version %d, actual %d", aggregateID, expected, actual),
	}
}
