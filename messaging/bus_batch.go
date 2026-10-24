package messaging

import (
	"context"

	"gochen/errors"
)

type publishAllBatchMiddleware interface {
	BeginPublishAllBatch(ctx context.Context) (context.Context, func(success bool))
}

type publishAllAttemptMiddleware interface {
	BeginPublishAllAttempt(ctx context.Context) func(success bool)
}

// publishAllMiddleware 包裹一层批量中间件，负责在失败时撤销本层产生的前缀。
//
// attempts 保存链上所有实现 publishAllAttemptMiddleware 的中间件；每一层进入时都
// 为全部 attempt 中间件注册检查点。这是回滚时序的正确性要求，不是冗余：
//
//   - 失败 unwind 时，位于失败层（含）以内各层的 defer 会立即以 success=false
//     回滚该次尝试产生的预留/提交回调/命令锁——外层恢复或重试型中间件发起重试时，
//     失败尝试的状态已被撤销（bus 契约："每层调用独立回滚"）。
//   - 若只在最外层注册一次检查点，回滚会被推迟到最外层 wrapper 返回之后，而重试
//     发生在此之前：外层重试会撞上尚未回滚的批次预留并被幂等层静默去重——批次无
//     重复项时命令从批次中丢失且 PublishAll 返回成功，破坏 at-least-once。
//
// 每层重复注册的代价是 N×M 次轻量检查点（一次 mutex 读 + len 比较），换取的
// 是与中间件位置无关的回滚时序；成功路径上 finish(success=true) 是无操作。
type publishAllMiddleware struct {
	IMiddleware
	messages *[]IMessage
	attempts []publishAllAttemptMiddleware
}

func (m publishAllMiddleware) Handle(ctx context.Context, message IMessage, next HandlerFunc) error {
	checkpoint := len(*m.messages)
	success := false
	defer func() {
		if !success {
			clear((*m.messages)[checkpoint:])
			*m.messages = (*m.messages)[:checkpoint]
		}
	}()
	for _, middleware := range m.attempts {
		if finish := middleware.BeginPublishAllAttempt(ctx); finish != nil {
			defer func() { finish(success) }()
		}
	}
	err := m.IMiddleware.Handle(ctx, message, next)
	// 内层短路或恢复错误可能返回 nil，却没有留下待投递消息；此时也撤销本次新增状态。
	success = err == nil && len(*m.messages) > checkpoint
	return err
}

// PublishAll 批量发布消息，并确保每条消息都先经过中间件链。
func (bus *MessageBus) PublishAll(ctx context.Context, messages []IMessage) error {
	if len(messages) == 0 {
		return nil
	}
	if bus == nil {
		return errors.NewCode(errors.InvalidInput, "bus is nil")
	}
	if bus.transport == nil {
		return errors.NewCode(errors.InvalidInput, "transport is nil")
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	if len(messages) == 1 {
		message := messages[0]
		if IsNilMessage(message) {
			return errors.NewCode(errors.InvalidInput, "message is nil").WithContext("index", 0)
		}

		// 与多消息路径共享批量作用域：实现 publishAllBatchMiddleware 的中间件
		//（如幂等批量标记）依赖 BeginPublishAllBatch 注入的批次上下文；缺失会
		// 导致单条批量语义与多条批量不一致，幂等闭环失效。
		middlewares := bus.snapshotMiddlewares()
		ctx, finishBatch := beginPublishAllBatch(ctx, middlewares)
		batchFinished := false
		defer func() {
			if !batchFinished {
				finishBatch(false)
			}
		}()
		for _, middleware := range middlewares {
			if publisher, ok := middleware.(interface {
				BeginPublish(context.Context) context.Context
			}); ok {
				ctx = publisher.BeginPublish(ctx)
			}
		}

		err := bus.executeMiddlewaresWith(ctx, message, func(ctx context.Context, msg IMessage) error {
			if IsNilMessage(msg) {
				return errors.NewCode(errors.InvalidInput, "middleware produced nil message").
					WithContext("index", 0).
					WithContext("message_id", message.GetID())
			}
			if pubErr := bus.transport.PublishAll(ctx, []IMessage{msg}); pubErr != nil {
				return wrapPublishError(pubErr, errors.Dependency, "failed to publish batch").WithContext("message_count", 1)
			}
			return nil
		}, middlewares)
		// 处理完成后无论成败都必须收尾批次，不再触发 defer 里的失败信号。
		batchFinished = true
		finishBatch(err == nil)
		if err != nil {
			return wrapPublishError(err, errors.Internal, "failed to publish message").
				WithContext("index", 0).
				WithContext("message_id", message.GetID())
		}

		return nil
	}

	batched := make([]IMessage, 0, len(messages))
	middlewares := bus.snapshotMiddlewares()
	ctx, finishBatch := beginPublishAllBatch(ctx, middlewares)
	batchFinished := false
	defer func() {
		if !batchFinished {
			finishBatch(false)
		}
	}()

	// 每层调用独立回滚，确保错误被外层恢复或重试时，失败尝试的消息和状态已撤销。
	// 恢复需要与中间件位置无关（失败层可能在幂等层之前或之后），因此每层挂载
	// attempts 列表并在进入时统一注册检查点（见 publishAllMiddleware.Handle）。
	var attempts []publishAllAttemptMiddleware
	for _, middleware := range middlewares {
		if attempt, ok := middleware.(publishAllAttemptMiddleware); ok {
			attempts = append(attempts, attempt)
		}
	}
	for i, middleware := range middlewares {
		middlewares[i] = publishAllMiddleware{
			IMiddleware: middleware,
			messages:    &batched,
			attempts:    attempts,
		}
	}

	for index, message := range messages {
		if IsNilMessage(message) {
			return errors.NewCode(errors.InvalidInput, "message is nil").WithContext("index", index)
		}

		err := bus.executeMiddlewaresWith(ctx, message, func(ctx context.Context, msg IMessage) error {
			if IsNilMessage(msg) {
				return errors.NewCode(errors.InvalidInput, "middleware produced nil message").
					WithContext("index", index).
					WithContext("message_id", message.GetID())
			}
			batched = append(batched, msg)
			return nil
		}, middlewares)
		if err != nil {
			return wrapPublishError(err, errors.Internal, "failed to publish message").
				WithContext("index", index).
				WithContext("message_id", message.GetID())
		}
	}

	if len(batched) == 0 {
		// 全部消息被中间件过滤（如幂等去重）属于成功完成，不是批次失败：
		// 必须以 success 收尾，否则外层 defer 会误广播批次失败信号，
		// 导致批次中间件误回滚或指标错误计数。
		finishBatch(true)
		batchFinished = true
		return nil
	}

	if err := bus.transport.PublishAll(ctx, batched); err != nil {
		return wrapPublishError(err, errors.Dependency, "failed to publish batch").
			WithContext("message_count", len(batched))
	}

	finishBatch(true)
	batchFinished = true
	return nil
}

func beginPublishAllBatch(ctx context.Context, middlewares []IMiddleware) (context.Context, func(success bool)) {
	finishers := make([]func(bool), 0)
	for _, middleware := range middlewares {
		batchMiddleware, ok := middleware.(publishAllBatchMiddleware)
		if !ok {
			continue
		}
		var finish func(bool)
		ctx, finish = batchMiddleware.BeginPublishAllBatch(ctx)
		if finish != nil {
			finishers = append(finishers, finish)
		}
	}
	return ctx, func(success bool) {
		for i := len(finishers) - 1; i >= 0; i-- {
			finishers[i](success)
		}
	}
}
