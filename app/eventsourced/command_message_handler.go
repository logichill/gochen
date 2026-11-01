package eventsourced

import (
	"context"
	"strings"

	"gochen/app/internal/depcheck"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/messaging"
	"gochen/messaging/command"
)

// ExecuteCommand 是命令总线执行命令的最小契约。
type ExecuteCommand[T any, ID comparable] interface {
	AggregateID() ID
	Apply(ctx context.Context, aggregate T) error
}

// CreateCommand 是命令总线新建命令的最小契约。
type CreateCommand[T any] interface {
	New(ctx context.Context) (T, error)
}

type executeMessageHandler[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	name        string
	app         *Application[T, ID]
	action      string
	commandType string
	decode      func(*command.Command) (ExecuteCommand[T, ID], error)
}

func (h *executeMessageHandler[T, ID]) Handle(ctx context.Context, message messaging.IMessage) error {
	c, ok := matchCommand(message, h.commandType)
	if !ok {
		return nil
	}
	if h.app == nil {
		return errors.NewCode(errors.InvalidInput, "eventsourced application cannot be nil")
	}
	cmd, err := h.decode(c)
	if err != nil {
		return err
	}
	if depcheck.IsNil(cmd) {
		return errors.NewCode(errors.InvalidInput, "decoded command cannot be nil")
	}
	_, err = h.app.Execute(ctx, h.action, cmd.AggregateID(), cmd.Apply)
	return err
}

func (h *executeMessageHandler[T, ID]) Type() string { return h.name }

type createMessageHandler[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	name        string
	app         *Application[T, ID]
	action      string
	commandType string
	decode      func(*command.Command) (CreateCommand[T], error)
}

func (h *createMessageHandler[T, ID]) Handle(ctx context.Context, message messaging.IMessage) error {
	c, ok := matchCommand(message, h.commandType)
	if !ok {
		return nil
	}
	if h.app == nil {
		return errors.NewCode(errors.InvalidInput, "eventsourced application cannot be nil")
	}
	cmd, err := h.decode(c)
	if err != nil {
		return err
	}
	if depcheck.IsNil(cmd) {
		return errors.NewCode(errors.InvalidInput, "decoded command cannot be nil")
	}
	_, err = h.app.ExecuteNew(ctx, h.action, cmd.New)
	return err
}

func (h *createMessageHandler[T, ID]) Type() string { return h.name }

// AsExecuteHandler 把命令消息适配为 Application.Execute。
func AsExecuteHandler[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	name string,
	commandType string,
	app *Application[T, ID],
	action string,
	decode func(*command.Command) (ExecuteCommand[T, ID], error),
) messaging.IMessageHandler {
	return &executeMessageHandler[T, ID]{
		name:        handlerName(name, "app.eventsourced.execute_handler"),
		app:         app,
		action:      action,
		commandType: commandType,
		decode:      decode,
	}
}

// AsExecuteNewHandler 把命令消息适配为 Application.ExecuteNew。
func AsExecuteNewHandler[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	name string,
	commandType string,
	app *Application[T, ID],
	action string,
	decode func(*command.Command) (CreateCommand[T], error),
) messaging.IMessageHandler {
	return &createMessageHandler[T, ID]{
		name:        handlerName(name, "app.eventsourced.execute_new_handler"),
		app:         app,
		action:      action,
		commandType: commandType,
		decode:      decode,
	}
}

func matchCommand(message messaging.IMessage, commandType string) (*command.Command, bool) {
	if message == nil || message.GetKind() != messaging.KindCommand {
		return nil, false
	}
	c, ok := message.(*command.Command)
	if !ok || c == nil {
		return nil, false
	}
	if commandType != "" && c.GetType() != commandType {
		return nil, false
	}
	return c, true
}

func handlerName(name, fallback string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return fallback
}
