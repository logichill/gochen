package eventsourced

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"gochen/domain/entity"
	repo "gochen/domain/repository"
	"gochen/eventing/bus"
	"gochen/logging"
	"gochen/messaging"
	cmd "gochen/messaging/command"
)

// IEventSourcedCommand 命令需要提供聚合 ID
type IEventSourcedCommand interface {
	AggregateID() int64
}

// EventSourcedCommandHandler 命令处理器
type EventSourcedCommandHandler[T entity.IEventSourcedAggregate[int64]] func(ctx context.Context, cmd IEventSourcedCommand, aggregate T) error

// EventSourcedServiceOptions 服务配置
type EventSourcedServiceOptions[T entity.IEventSourcedAggregate[int64]] struct {
	EventBus       bus.IEventBus
	Logger         logging.ILogger
	CommandHooks   []EventSourcedCommandHook[T]
	ICommandTracer ICommandTracer
}

// EventSourcedCommandHook 命令执行钩子
type EventSourcedCommandHook[T entity.IEventSourcedAggregate[int64]] interface {
	BeforeExecute(ctx context.Context, cmd IEventSourcedCommand, agg T) error
	AfterExecute(ctx context.Context, cmd IEventSourcedCommand, agg T, execErr error) error
}

// ICommandTracer 提供执行耗时等指标记录
type ICommandTracer interface {
	Trace(ctx context.Context, commandName string, elapsed time.Duration, err error)
}

// EventSourcedService 统一的命令执行模板
type EventSourcedService[T entity.IEventSourcedAggregate[int64]] struct {
	repository repo.IEventSourcedRepository[T, int64]
	handlers   map[reflect.Type]EventSourcedCommandHandler[T]
	eventBus   bus.IEventBus
	logger     logging.ILogger
	hooks      []EventSourcedCommandHook[T]
	tracer     ICommandTracer
}

// NewEventSourcedService 创建事件溯源服务模板
func NewEventSourcedService[T entity.IEventSourcedAggregate[int64]](
	repository repo.IEventSourcedRepository[T, int64],
	opts *EventSourcedServiceOptions[T],
) (*EventSourcedService[T], error) {
	if repository == nil {
		return nil, fmt.Errorf("repository cannot be nil")
	}
	service := &EventSourcedService[T]{
		repository: repository,
		handlers:   make(map[reflect.Type]EventSourcedCommandHandler[T]),
	}
	if opts != nil {
		service.eventBus = opts.EventBus
		service.hooks = opts.CommandHooks
		service.tracer = opts.ICommandTracer
		service.logger = opts.Logger
	}
	if service.logger == nil {
		service.logger = logging.GetLogger().WithFields(
			logging.String("component", "eventsourced.service"),
		)
	}
	return service, nil
}

// RegisterCommandHandler 注册命令处理器
func (s *EventSourcedService[T]) RegisterCommandHandler(prototype IEventSourcedCommand, handler EventSourcedCommandHandler[T]) error {
	if prototype == nil {
		return fmt.Errorf("command prototype cannot be nil")
	}
	if handler == nil {
		return fmt.Errorf("command handler cannot be nil")
	}
	cmdType := reflect.TypeOf(prototype)
	if cmdType.Kind() != reflect.Ptr {
		return fmt.Errorf("command prototype must be pointer type, got %s", cmdType.String())
	}
	s.handlers[cmdType] = handler
	return nil
}

// ExecuteCommand 执行命令
func (s *EventSourcedService[T]) ExecuteCommand(ctx context.Context, cmd IEventSourcedCommand) error {
	if cmd == nil {
		return fmt.Errorf("command cannot be nil")
	}
	cmdType := reflect.TypeOf(cmd)
	handler, exists := s.handlers[cmdType]
	if !exists {
		return fmt.Errorf("command handler not found for type %s", cmdType.String())
	}

	aggregateID := cmd.AggregateID()
	if aggregateID <= 0 {
		return fmt.Errorf("invalid aggregate id %d", aggregateID)
	}

	aggregate, err := s.repository.GetByID(ctx, aggregateID)
	if err != nil {
		return fmt.Errorf("load aggregate failed: %w", err)
	}

	start := time.Now()

	for _, hook := range s.hooks {
		if err := hook.BeforeExecute(ctx, cmd, aggregate); err != nil {
			return fmt.Errorf("before execute hook failed: %w", err)
		}
	}

	execErr := handler(ctx, cmd, aggregate)

	for _, hook := range s.hooks {
		if hookErr := hook.AfterExecute(ctx, cmd, aggregate, execErr); hookErr != nil {
			s.logger.Warn(ctx, "after execute hook failed",
				logging.Error(hookErr),
				logging.String("command", cmdType.String()))
		}
	}

	if execErr != nil {
		s.trace(ctx, cmdType.String(), time.Since(start), execErr)
		return execErr
	}

	if err := s.repository.Save(ctx, aggregate); err != nil {
		s.trace(ctx, cmdType.String(), time.Since(start), err)
		return err
	}

	s.trace(ctx, cmdType.String(), time.Since(start), nil)
	return nil
}

func (s *EventSourcedService[T]) trace(ctx context.Context, commandName string, elapsed time.Duration, execErr error) {
	if s.tracer != nil {
		s.tracer.Trace(ctx, commandName, elapsed, execErr)
	}
}

// AsCommandMessageHandler 将已注册的命令处理逻辑适配为 IMessageHandler，供 Command 总线或 MessageBus 订阅。
//
// 使用方式：
//
//	handler := service.AsCommandMessageHandler("SetValue", func(c *cmd.Command) (IEventSourcedCommand, error) {
//	    // 将 *cmd.Command 转换为领域命令（需实现 IEventSourcedCommand）
//	    payload, _ := c.GetPayload().(int)
//	    return &SetValue{ID: c.GetAggregateID(), V: payload}, nil
//	})
//	messageBus.Subscribe(ctx, messaging.MessageTypeCommand, handler)
//
// 说明：
// - 订阅时建议使用 messaging.MessageTypeCommand（或 "*" 通配）作为消息类型；
// - 该适配器会根据 command.Metadata["command_type"] 与入参 commandType 进行匹配，避免串扰。
type commandMessageHandler[T entity.IEventSourcedAggregate[int64]] struct {
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

// AsCommandMessageHandler 见注释：将服务适配为 IMessageHandler
func (s *EventSourcedService[T]) AsCommandMessageHandler(commandType string, factory func(*cmd.Command) (IEventSourcedCommand, error)) messaging.IMessageHandler {
	return &commandMessageHandler[T]{
		name:        "eventsourced.service.command_adapter",
		service:     s,
		commandType: commandType,
		factory:     factory,
	}
}
