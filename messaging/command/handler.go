package command

import (
	"context"
	"fmt"

	gerrors "gochen/errors"
	"gochen/messaging"
)

// CommandHandlerFunc 命令处理函数类型。
//
// 这是一个便利类型，允许使用函数作为命令处理器。
type CommandHandlerFunc func(ctx context.Context, cmd *Command) error

// AsMessageHandler 把函数式命令处理器适配为通用消息处理器。
func (f CommandHandlerFunc) AsMessageHandler(name string) messaging.IMessageHandler {
	return &messageHandlerFunc{
		handleFunc: f,
		name:       name,
	}
}

// messageHandlerFunc 把 CommandHandlerFunc 包装成 IMessageHandler。
type messageHandlerFunc struct {
	handleFunc CommandHandlerFunc
	name       string
}

// Handle 校验消息类型后把它交给底层命令处理函数执行。
func (h *messageHandlerFunc) Handle(ctx context.Context, message messaging.IMessage) error {
	// 类型断言：确保消息是命令
	cmd, ok := message.(*Command)
	if !ok {
		return gerrors.NewCode(gerrors.InvalidInput, "expected *Command").
			WithContext("message_type", fmt.Sprintf("%T", message))
	}

	// 调用实际的命令处理函数
	return h.handleFunc(ctx, cmd)
}

func (h *messageHandlerFunc) Type() string {
	return h.name
}

// TypedCommandHandler 提供带 payload 泛型约束的命令处理器适配。
type TypedCommandHandler[T any] struct {
	handleFunc func(ctx context.Context, cmd *Command, payload T) error
	name       string
}

// NewTypedCommandHandler 创建一个会自动解析 payload 类型的命令处理器。
func NewTypedCommandHandler[T any](name string, fn func(ctx context.Context, cmd *Command, payload T) error) *TypedCommandHandler[T] {
	return &TypedCommandHandler[T]{
		handleFunc: fn,
		name:       name,
	}
}

// Handle 校验命令类型并把 payload 解码成目标泛型类型后执行处理函数。
func (h *TypedCommandHandler[T]) Handle(ctx context.Context, message messaging.IMessage) error {
	cmd, ok := message.(*Command)
	if !ok {
		return gerrors.NewCode(gerrors.InvalidInput, "expected *Command").
			WithContext("message_type", fmt.Sprintf("%T", message))
	}

	// 类型断言 Payload
	payload, ok := messaging.PayloadAs[T](cmd.GetPayload())
	if !ok {
		return gerrors.NewCode(gerrors.InvalidInput, "invalid payload type").
			WithContext("expected_payload_type", fmt.Sprintf("%T", *new(T))).
			WithContext("actual_payload_type", cmd.GetPayload().TypeName())
	}

	return h.handleFunc(ctx, cmd, payload)
}

func (h *TypedCommandHandler[T]) Type() string {
	return h.name
}

// AsMessageHandler 直接返回自身，便于作为 IMessageHandler 注入总线。
func (h *TypedCommandHandler[T]) AsMessageHandler() messaging.IMessageHandler {
	return h
}
