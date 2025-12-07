package eventsourced

import (
	"context"

	deventsourced "gochen/domain/eventsourced"
	"gochen/messaging"
	cmd "gochen/messaging/command"
)

// commandMessageHandler 将领域层 EventSourcedService 适配为 IMessageHandler。
type commandMessageHandler[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	name        string
	service     *deventsourced.EventSourcedService[T, ID]
	commandType string
	factory     func(*cmd.Command) (deventsourced.IEventSourcedCommand[ID], error)
}

func (h *commandMessageHandler[T, ID]) Handle(ctx context.Context, message messaging.IMessage) error {
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

func (h *commandMessageHandler[T, ID]) Type() string { return h.name }

// AsCommandMessageHandler 将领域层服务适配为 IMessageHandler，供 Command 总线或 MessageBus 订阅。
func AsCommandMessageHandler[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	service *deventsourced.EventSourcedService[T, ID],
	commandType string,
	factory func(*cmd.Command) (deventsourced.IEventSourcedCommand[ID], error),
) messaging.IMessageHandler {
	return &commandMessageHandler[T, ID]{
		name:        "app.eventsourced.service.command_adapter",
		service:     service,
		commandType: commandType,
		factory:     factory,
	}
}

