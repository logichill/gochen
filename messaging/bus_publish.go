package messaging

import (
	"context"

	"gochen/contextx"
	"gochen/errors"
)

// Publish 发布单条消息，并在真正投递前执行中间件链。
func (bus *MessageBus) Publish(ctx context.Context, message IMessage) error {
	if bus == nil {
		return errors.NewCode(errors.InvalidInput, "bus is nil")
	}
	if bus.transport == nil {
		return errors.NewCode(errors.InvalidInput, "transport is nil")
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if IsNilMessage(message) {
		return errors.NewCode(errors.InvalidInput, "message is nil")
	}
	finalHandler := func(ctx context.Context, msg IMessage) error {
		return bus.transport.Publish(ctx, msg)
	}
	return bus.executeMiddlewares(ctx, message, finalHandler)
}

// wrapPublishError 保留完整错误链及显式错误码，未分类错误使用所在边界的默认码。
func wrapPublishError(err error, fallback errors.ErrorCode, message string) errors.IAppError {
	if appErr, ok := err.(*errors.AppError); ok && appErr != nil {
		return appErr
	}
	// Normalize 仅为可识别的错误附加业务码；普通错误保持原样。
	var appErr *errors.AppError
	if normalized := errors.Normalize(err); errors.As(normalized, &appErr) && appErr != nil {
		fallback = errors.Code(normalized)
	}
	return errors.Wrap(err, fallback, message)
}

// executeMiddlewares 隔离继承的批次状态，再构建并执行即时投递处理链。
func (bus *MessageBus) executeMiddlewares(ctx context.Context, message IMessage, finalHandler HandlerFunc) error {
	middlewares := bus.snapshotMiddlewares()
	for _, middleware := range middlewares {
		if publisher, ok := middleware.(interface {
			BeginPublish(context.Context) context.Context
		}); ok {
			ctx = publisher.BeginPublish(ctx)
		}
	}
	return bus.executeMiddlewaresWith(ctx, message, finalHandler, middlewares)
}

func (bus *MessageBus) executeMiddlewaresWith(ctx context.Context, message IMessage, finalHandler HandlerFunc, middlewares []IMiddleware) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	// 默认贯通：将 metadata 中的链路信息（tenant/trace/operator）与 message.Metadata 双向补齐。
	// 说明：
	// - Publish：确保 metadata 携带关键字段，跨进程可关联；
	// - Consume：若 Transport 未透传 ctx，该信息也可从 metadata 派生回来（见 handlerWithErrorHook.Handle）。
	// 优先级约定：
	// - ctx 优先：同进程内不允许 metadata 覆盖 ctx（避免链路排障混乱）；
	// - metadata 仅用于“ctx 缺失时补齐”（典型：跨进程/异步 Transport 未透传 ctx）。
	if message != nil {
		md := message.GetMetadata()
		var err error
		ctx, err = contextx.DeriveFromMetadata(ctx, md)
		if err != nil {
			return err
		}
		ctx, err = contextx.EnsureTraceID(ctx, md, message.GetID())
		if err != nil {
			return err
		}
		if err := contextx.InjectTenantID(ctx, md); err != nil {
			return err
		}
		if err := contextx.InjectOperator(ctx, md); err != nil {
			return err
		}
	}

	if len(middlewares) == 0 {
		return finalHandler(ctx, message)
	}

	next := finalHandler
	for i := len(middlewares) - 1; i >= 0; i-- {
		middleware := middlewares[i]
		currentNext := next
		next = func(ctx context.Context, msg IMessage) error {
			return middleware.Handle(ctx, msg, currentNext)
		}
	}
	return next(ctx, message)
}
