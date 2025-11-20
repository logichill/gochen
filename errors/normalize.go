package errors

import (
	stdErrors "errors"

	"gochen/domain/entity"
	repository "gochen/domain/repository"
	"gochen/eventing"
	cmd "gochen/messaging/command"
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
	if stdErrors.Is(err, eventing.ErrAggregateNotFound) {
		return WrapError(err, ErrCodeNotFound, "聚合未找到")
	}

	var concurrencyErr *eventing.ConcurrencyError
	if stdErrors.As(err, &concurrencyErr) {
		return WrapError(err, ErrCodeConcurrency, "事件存储并发冲突")
	}

	// 领域实体/仓储错误
	if stdErrors.Is(err, entity.ErrAggregateNotFound) || stdErrors.Is(err, repository.ErrEntityNotFound) {
		return WrapError(err, ErrCodeNotFound, "实体未找到")
	}

	if stdErrors.Is(err, repository.ErrVersionConflict) {
		return WrapError(err, ErrCodeConcurrency, "仓储版本冲突")
	}

	// 命令总线常见错误
	if stdErrors.Is(err, cmd.ErrAggregateNotFound) {
		return WrapError(err, ErrCodeNotFound, "命令目标聚合未找到")
	}
	if stdErrors.Is(err, cmd.ErrConcurrencyConflict) {
		return WrapError(err, ErrCodeConcurrency, "命令处理并发冲突")
	}
	if stdErrors.Is(err, cmd.ErrInvalidCommand) {
		return WrapError(err, ErrCodeInvalidInput, "无效的命令")
	}

	// 未识别的错误保持原样
	return err
}

