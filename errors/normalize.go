package errors

import (
	stdErrors "errors"

	"gochen/domain"
	"gochen/eventing"
	"gochen/messaging/command"
)

// Normalize 将领域层/基础设施层的错误规范化为 AppError。
//
// 设计目标：
//   - 对外统一暴露 ErrorCode 体系，避免 HTTP 层出现一堆“裸”错误类型；
//   - 保留原始错误作为 cause，方便日志与调试；
//   - 仅处理当前框架中常见的错误类型，后续可按需扩展（例如 ISql 相关错误）。
//
// 注意：
//   - 如果传入的 err 已经是 IError，则原样返回；
//   - 未识别的错误保持原样，不强行包装，交由调用方决定是否 Wrap。
func Normalize(err error) error {
	if err == nil {
		return nil
	}

	// 已经是 AppError，直接返回
	if _, ok := err.(IError); ok {
		return err
	}

	// 事件存储相关错误
	if stdErrors.Is(err, eventing.ErrAggregateNotFound()) {
		return WrapError(err, ErrCodeNotFound, "aggregate not found")
	}

	var concurrencyErr *eventing.ConcurrencyError
	if stdErrors.As(err, &concurrencyErr) {
		return WrapError(err, ErrCodeConcurrency, "event store concurrency conflict")
	}

	// 领域实体/仓储错误
	if stdErrors.Is(err, eventing.ErrAggregateNotFound()) || stdErrors.Is(err, domain.ErrEntityNotFound()) {
		return WrapError(err, ErrCodeNotFound, "entity not found")
	}

	if stdErrors.Is(err, domain.ErrVersionConflict()) {
		return WrapError(err, ErrCodeConcurrency, "repository version conflict")
	}

	// 命令总线常见错误
	if stdErrors.Is(err, command.ErrAggregateNotFound()) {
		return WrapError(err, ErrCodeNotFound, "command target aggregate not found")
	}
	if stdErrors.Is(err, command.ErrConcurrencyConflict()) {
		return WrapError(err, ErrCodeConcurrency, "command processing concurrency conflict")
	}
	if stdErrors.Is(err, command.ErrInvalidCommand()) {
		return WrapError(err, ErrCodeInvalidInput, "invalid command")
	}

	// 未识别的错误保持原样
	return err
}
