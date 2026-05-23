package command

import (
	"context"

	"gochen/errors"
	"gochen/messaging"
)

// ICommandDispatcher 抽象“命令投递”端口。
//
// 语义约定：
// - Dispatch 只保证命令进入消息总线 / transport；
// - 是否同步执行、handler 是否已经完成，由底层 transport 决定；
// - 适合异步消息驱动或只关心投递结果的调用方。
type ICommandDispatcher interface {
	Dispatch(ctx context.Context, cmd *Command) error
}

// CommandBus 是命令投递端口，对 MessageBus 做薄封装。
//
// 它只保留“把命令作为消息发布出去”的职责，不再承载本地 handler 注册或同步执行语义。
type CommandBus struct {
	messageBus messaging.IMessageBus
}

// NewCommandBus 创建命令投递总线。
func NewCommandBus(messageBus messaging.IMessageBus) *CommandBus {
	return &CommandBus{messageBus: messageBus}
}

// Dispatch 投递命令到消息总线。
func (bus *CommandBus) Dispatch(ctx context.Context, cmd *Command) error {
	if bus == nil || bus.messageBus == nil {
		return errors.NewCode(errors.InvalidInput, "message bus cannot be nil")
	}
	if cmd == nil {
		return errors.NewCode(errors.InvalidInput, "command cannot be nil")
	}
	return bus.messageBus.Publish(ctx, cmd)
}

// Use 注册投递侧中间件。
func (bus *CommandBus) Use(middleware messaging.IMiddleware) {
	if bus == nil || bus.messageBus == nil || middleware == nil {
		return
	}
	bus.messageBus.Use(middleware)
}

var _ ICommandDispatcher = (*CommandBus)(nil)
