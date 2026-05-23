package eventsourced

import (
	"context"

	deventsourced "gochen/domain/eventsourced"
	"gochen/messaging"
	cmd "gochen/messaging/command"
)

// commandMessageHandler 将应用层 EventSourcedService 适配为 IMessageHandler。
type commandMessageHandler[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	name        string
	service     *EventSourcedService[T, ID]
	commandType string
	factory     func(*cmd.Command) (IEventSourcedCommand[ID], error)
}

func (h *commandMessageHandler[T, ID]) Handle(ctx context.Context, message messaging.IMessage) error {
	if message.GetKind() != messaging.KindCommand {
		return nil
	}
	c, ok := message.(*cmd.Command)
	if !ok {
		return nil
	}
	mt := c.GetType()
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

func AsCommandMessageHandler[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	service *EventSourcedService[T, ID],
	commandType string,
	factory func(*cmd.Command) (IEventSourcedCommand[ID], error),
) messaging.IMessageHandler {
	return &commandMessageHandler[T, ID]{
		name:        "app.eventsourced.service.command_handler",
		service:     service,
		commandType: commandType,
		factory:     factory,
	}
}
