package eventsourced

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"gochen/logging"
)

// EventSourcedService 统一的命令执行模板。
//
// 该服务基于领域层的事件溯源仓储与命令处理器，封装了：
//   - 加载聚合；
//   - 执行命令（含前后钩子与追踪）；
//   - 保存聚合（由 IEventSourcedRepository 实现具体持久化策略）。
//
// 注意：该实现仅依赖领域抽象与日志接口，不直接依赖 EventStore、EventBus 等基础设施。
//
// 类型参数：
//   - T: 聚合根类型
//   - ID: 聚合根 ID 类型，必须是可比较类型
type EventSourcedService[T IEventSourcedAggregate[ID], ID comparable] struct {
	repository IEventSourcedRepository[T, ID]
	handlers   map[reflect.Type]EventSourcedCommandHandler[T, ID]
	logger     logging.ILogger
	hooks      []EventSourcedCommandHook[T, ID]
	tracer     ICommandTracer
}

// NewEventSourcedService 创建事件溯源服务模板。
func NewEventSourcedService[T IEventSourcedAggregate[ID], ID comparable](
	repository IEventSourcedRepository[T, ID],
	opts *EventSourcedServiceOptions[T, ID],
) (*EventSourcedService[T, ID], error) {
	if repository == nil {
		return nil, fmt.Errorf("repository cannot be nil")
	}
	service := &EventSourcedService[T, ID]{
		repository: repository,
		handlers:   make(map[reflect.Type]EventSourcedCommandHandler[T, ID]),
	}
	if opts != nil {
		service.hooks = opts.CommandHooks
		service.tracer = opts.ICommandTracer
		service.logger = opts.Logger
	}
	if service.logger == nil {
		service.logger = logging.ComponentLogger("eventsourced.service")
	}
	return service, nil
}

// RegisterCommandHandler 注册命令处理器。
func (s *EventSourcedService[T, ID]) RegisterCommandHandler(prototype IEventSourcedCommand[ID], handler EventSourcedCommandHandler[T, ID]) error {
	if prototype == nil {
		return fmt.Errorf("command prototype cannot be nil")
	}
	cmdType := reflect.TypeOf(prototype)
	if cmdType.Kind() != reflect.Ptr {
		return fmt.Errorf("command prototype must be pointer type, got %s", cmdType.String())
	}
	s.handlers[cmdType] = handler
	return nil
}

// ExecuteCommand 执行命令。
func (s *EventSourcedService[T, ID]) ExecuteCommand(ctx context.Context, cmd IEventSourcedCommand[ID]) error {
	if cmd == nil {
		return fmt.Errorf("command cannot be nil")
	}
	cmdType := reflect.TypeOf(cmd)
	handler, exists := s.handlers[cmdType]
	if !exists {
		return fmt.Errorf("command handler not found for type %s", cmdType.String())
	}

	aggregateID := cmd.AggregateID()

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
			if s.logger != nil {
				s.logger.Warn(ctx, "after execute hook failed",
					logging.Error(hookErr),
					logging.String("command", cmdType.String()))
			}
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

func (s *EventSourcedService[T, ID]) trace(ctx context.Context, commandName string, elapsed time.Duration, execErr error) {
	if s.tracer != nil {
		s.tracer.Trace(ctx, commandName, elapsed, execErr)
	}
}

