package command

import "errors"

var (
	// ErrInvalidCommand 无效的命令
	ErrInvalidCommand = errors.New("invalid command")

	// ErrCommandHandlerNotFound 命令处理器未找到
	ErrCommandHandlerNotFound = errors.New("command handler not found")

	// ErrCommandHandlerAlreadyRegistered 命令处理器已注册
	ErrCommandHandlerAlreadyRegistered = errors.New("command handler already registered")

	// ErrInvalidCommandType 无效的命令类型
	ErrInvalidCommandType = errors.New("invalid command type")

	// ErrCommandExecutionFailed 命令执行失败
	ErrCommandExecutionFailed = errors.New("command execution failed")

	// ErrAggregateNotFound 聚合未找到
	ErrAggregateNotFound = errors.New("aggregate not found")

	// ErrConcurrencyConflict 并发冲突
	ErrConcurrencyConflict = errors.New("concurrency conflict")
)
