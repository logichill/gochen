package command

import (
	"context"
	"fmt"

	"gochen/messaging"
)

// CommandHandlerFunc 命令处理函数类型
//
// 这是一个便利类型，允许使用函数作为命令处理器
type CommandHandlerFunc func(ctx context.Context, cmd *Command) error

// AsMessageHandler 转换为 IMessageHandler
//
// 将命令处理函数适配为消息处理器，以便在 MessageBus 中使用
//
// 参数：
//   - name: 处理器名称（用于日志和调试）
//
// 返回：
//   - messaging.IMessageHandler: 适配后的消息处理器
func (f CommandHandlerFunc) AsMessageHandler(name string) messaging.IMessageHandler {
	return &commandHandlerAdapter{
		handleFunc: f,
		name:       name,
	}
}

// commandHandlerAdapter 命令处理器适配器
//
// 将 CommandHandlerFunc 适配为 IMessageHandler 接口
type commandHandlerAdapter struct {
	handleFunc CommandHandlerFunc
	name       string
}

// Handle 实现 IMessageHandler 接口
//
// 将消息转换为命令并调用实际的处理函数
func (h *commandHandlerAdapter) Handle(ctx context.Context, message messaging.IMessage) error {
	// 类型断言：确保消息是命令
	cmd, ok := message.(*Command)
	if !ok {
		return fmt.Errorf("expected *Command, got %T", message)
	}

	// 调用实际的命令处理函数
	return h.handleFunc(ctx, cmd)
}

// Type 实现 IMessageHandler 接口
func (h *commandHandlerAdapter) Type() string {
	return h.name
}

// TypedCommandHandler 类型化命令处理器
//
// 提供泛型支持的命令处理器（Go 1.18+）
// 允许处理特定 Payload 类型的命令
type TypedCommandHandler[T any] struct {
	handleFunc func(ctx context.Context, cmd *Command, payload T) error
	name       string
}

// NewTypedCommandHandler 创建类型化命令处理器
func NewTypedCommandHandler[T any](name string, fn func(ctx context.Context, cmd *Command, payload T) error) *TypedCommandHandler[T] {
	return &TypedCommandHandler[T]{
		handleFunc: fn,
		name:       name,
	}
}

// Handle 实现 IMessageHandler 接口
func (h *TypedCommandHandler[T]) Handle(ctx context.Context, message messaging.IMessage) error {
	cmd, ok := message.(*Command)
	if !ok {
		return fmt.Errorf("expected *Command, got %T", message)
	}

	// 类型断言 Payload
	payload, ok := cmd.GetPayload().(T)
	if !ok {
		return fmt.Errorf("invalid payload type: expected %T, got %T", *new(T), cmd.GetPayload())
	}

	return h.handleFunc(ctx, cmd, payload)
}

// Type 实现 IMessageHandler 接口
func (h *TypedCommandHandler[T]) Type() string {
	return h.name
}

// AsMessageHandler 转换为 IMessageHandler
func (h *TypedCommandHandler[T]) AsMessageHandler() messaging.IMessageHandler {
	return h
}
