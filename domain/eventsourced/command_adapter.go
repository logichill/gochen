package eventsourced

import (
	"context"

	"gochen/messaging"
	cmd "gochen/messaging/command"
)

// commandMessageHandler 将 EventSourcedService 适配为 IMessageHandler
type commandMessageHandler[T IEventSourcedAggregate[int64]] struct {
	name        string
	service     *EventSourcedService[T]
	commandType string
	factory     func(*cmd.Command) (IEventSourcedCommand, error)
}

func (h *commandMessageHandler[T]) Handle(ctx context.Context, message messaging.IMessage) error {
	if message.GetType() != messaging.MessageTypeCommand {
		return nil
	}
	c, ok := message.(*cmd.Command)
	if !ok {
		return nil
	}
	mt, _ := c.GetMetadata()["command_type"].(string)
	if h.commandType != "" && mt != h.commandType {
		return nil
	}
	domainCmd, err := h.factory(c)
	if err != nil {
		return err
	}
	return h.service.ExecuteCommand(ctx, domainCmd)
}

func (h *commandMessageHandler[T]) Type() string { return h.name }

// AsCommandMessageHandler 将服务适配为 IMessageHandler，供 Command 总线或 MessageBus 订阅。
func (s *EventSourcedService[T]) AsCommandMessageHandler(commandType string, factory func(*cmd.Command) (IEventSourcedCommand, error)) messaging.IMessageHandler {
	return &commandMessageHandler[T]{
		name:        "eventsourced.service.command_adapter",
		service:     s,
		commandType: commandType,
		factory:     factory,
	}
}
