package eventsourced

import (
	"context"
	"reflect"
	"sync"
	"time"

	"gochen/app/internal/commandflow"
	"gochen/app/internal/depcheck"
	"gochen/app/operation"
	"gochen/contextx"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/observe/logging"
	"gochen/policy/retry"
)

// EventSourcedService 统一的事件溯源命令执行模板（应用层）。
//
// 该服务基于领域层的事件溯源仓储与命令处理器，封装了：
//   - 加载聚合；
//   - 执行命令（含前后钩子与追踪）；
//   - 保存聚合（由 IEventSourcedRepository 实现具体持久化策略）。
//
// 类型参数：
//   - T: 聚合根类型。
//   - ID: 聚合根 ID 类型，必须是可比较类型。
type EventSourcedService[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	repository deventsourced.IEventSourcedRepository[T, ID]
	mu         sync.RWMutex // 保护 handlers map 的并发访问
	handlers   map[reflect.Type]EventSourcedCommandHandler[T, ID]
	logger     logging.ILogger
	hooks      []IEventSourcedCommandHook[T, ID]
	tracer     ICommandTracer

	operationRunner         operation.IRunner
	operationSpecResolver   OperationSpecResolver[ID]
	operationResultResolver OperationResultResolver[ID]

	retryConfig        *RetryConfig
	isConcurrencyError IsConcurrencyError
}

// NewEventSourcedService 创建一个事件溯源命令执行服务。
func NewEventSourcedService[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	repository deventsourced.IEventSourcedRepository[T, ID],
	opts *EventSourcedServiceOptions[T, ID],
) (*EventSourcedService[T, ID], error) {
	if depcheck.IsNil(repository) {
		return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
	}
	service := &EventSourcedService[T, ID]{
		repository: repository,
		handlers:   make(map[reflect.Type]EventSourcedCommandHandler[T, ID]),
		// 默认不启用重试；当 opts.ConcurrencyRetry 非 nil 时启用。
		retryConfig:        nil,
		isConcurrencyError: DefaultIsConcurrencyError,
	}
	if opts != nil {
		service.hooks = filterCommandHooks(opts.CommandHooks)
		if !depcheck.IsNil(opts.CommandTracer) {
			service.tracer = opts.CommandTracer
		}
		if !depcheck.IsNil(opts.Logger) {
			service.logger = opts.Logger
		}
		if !depcheck.IsNil(opts.OperationRunner) {
			service.operationRunner = opts.OperationRunner
		}
		service.operationSpecResolver = opts.OperationSpecResolver
		service.operationResultResolver = opts.OperationResultResolver
		service.retryConfig = normalizeRetryConfig(opts.ConcurrencyRetry)
		if opts.IsConcurrencyError != nil {
			service.isConcurrencyError = opts.IsConcurrencyError
		}
	}
	if service.logger == nil {
		service.logger = logging.ComponentLogger("app.eventsourced.command_service")
	}
	return service, nil
}

// RegisterCommandHandler 为某个命令类型注册对应的处理器。
// prototype 仅作为类型标记，允许带类型的 nil 指针。该方法可并发调用。
func (s *EventSourcedService[T, ID]) RegisterCommandHandler(prototype IEventSourcedCommand[ID], handler EventSourcedCommandHandler[T, ID]) error {
	if prototype == nil {
		return errors.NewCode(errors.InvalidInput, "command prototype cannot be nil")
	}
	if handler == nil {
		return errors.NewCode(errors.InvalidInput, "command handler cannot be nil")
	}
	cmdType := reflect.TypeOf(prototype)
	if cmdType.Kind() != reflect.Ptr {
		return errors.NewCode(errors.InvalidInput, "command prototype must be pointer type").
			WithContext("command_type", cmdType.String())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.handlers[cmdType]; exists {
		return errors.NewCode(errors.Conflict, "command handler already registered").
			WithContext("command_type", cmdType.String())
	}
	s.handlers[cmdType] = handler
	return nil
}

// ExecuteCommand 完成一次“加载聚合 -> 执行业务 -> 保存聚合”的命令执行流程。
func (s *EventSourcedService[T, ID]) ExecuteCommand(ctx context.Context, cmd IEventSourcedCommand[ID]) error {
	_, err := s.executeCommand(ctx, cmd)
	return err
}

func (s *EventSourcedService[T, ID]) executeCommand(ctx context.Context, cmd IEventSourcedCommand[ID]) (T, error) {
	var zero T
	if s == nil {
		return zero, errors.NewCode(errors.InvalidInput, "event sourced service is nil")
	}
	if ctx == nil {
		return zero, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if depcheck.IsNil(cmd) {
		return zero, errors.NewCode(errors.InvalidInput, "command cannot be nil")
	}
	cmdType := reflect.TypeOf(cmd)
	s.mu.RLock()
	handler, exists := s.handlers[cmdType]
	s.mu.RUnlock()
	if !exists {
		return zero, errors.NewCode(errors.NotFound, "command handler not found").
			WithContext("command_type", cmdType.String())
	}

	aggregateID := cmd.AggregateID()
	commandName := cmdType.String()
	result, err := commandflow.Run(ctx, commandflow.Plan[T]{
		Attempt: func(opCtx context.Context, attempt int) (T, error) {
			_ = attempt
			return s.executeAttempt(opCtx, cmd, handler, aggregateID)
		},
		RetryConfig: s.commandRetryPolicy(),
		AfterAttempt: func(opCtx context.Context, attempt int, aggregate T, attemptErr error) {
			_ = attempt
			s.runAfterExecuteHooks(opCtx, cmd, commandName, aggregate, attemptErr)
		},
		AfterFinalize: func(opCtx context.Context, attempts int, aggregate T, finalErr error) {
			s.runAfterFinalizeHooks(opCtx, cmd, commandName, aggregate, finalErr, attempts)
		},
		OnRetry: func(opCtx context.Context, attempt int, aggregate T, attemptErr error, delay time.Duration) {
			_ = aggregate
			if s.logger == nil || s.retryConfig == nil {
				return
			}
			s.logger.Warn(opCtx, "concurrency conflict, retrying command",
				logging.Any("aggregate_id", aggregateID),
				logging.String("command", commandName),
				logging.Int("attempt", attempt),
				logging.Int("max_retries", s.retryConfig.MaxRetries),
				logging.Duration("backoff", delay),
				logging.Error(attemptErr))
		},
		WrapFinalError: func(finalErr error, attempts int) error {
			_ = attempts
			if finalErr == nil || contextx.IsAfterCommitError(finalErr) || s.retryConfig == nil || s.retryConfig.MaxRetries <= 0 || !s.isConcurrencyError(finalErr) {
				return finalErr
			}
			return &RetryExhaustedError{Cause: finalErr, MaxRetries: s.retryConfig.MaxRetries}
		},
		Trace: func(traceCtx context.Context, attempts int, elapsed time.Duration, finalErr error) {
			_ = attempts
			s.trace(traceCtx, commandName, elapsed, finalErr)
		},
	})
	return result.State, err
}

// executeAttempt 执行一次真实尝试，包括加载聚合、运行 hook、执行 handler 和保存。
func (s *EventSourcedService[T, ID]) executeAttempt(
	ctx context.Context,
	cmd IEventSourcedCommand[ID],
	handler EventSourcedCommandHandler[T, ID],
	aggregateID ID,
) (T, error) {
	aggregate, err := s.repository.GetOrCreate(ctx, aggregateID)
	if err != nil {
		return aggregate, s.wrapAggregateError(err, aggregateID)
	}

	if err := s.runBeforeExecuteHooks(ctx, cmd, aggregate); err != nil {
		return aggregate, err
	}

	execErr := handler(ctx, cmd, aggregate)
	finalErr := execErr
	if finalErr == nil {
		finalErr = s.repository.Save(ctx, aggregate)
	}
	if finalErr != nil {
		// 失败尝试可能已在聚合上部分应用了事件（handler 中途报错或 Save 乐观锁冲突）。
		// 丢弃未提交事件，避免脏状态被后续 Hook 误读为待提交事件；
		// 版本号保持不变，聚合实例仅作为只读诊断视图传给 Hook。
		clearUncommittedEvents(aggregate)
	}
	return aggregate, finalErr
}

// clearUncommittedEvents 通过领域接口清理失败尝试的未提交事件；
// 聚合未实现该接口（自定义实现）时静默跳过。
func clearUncommittedEvents[T deventsourced.IEventSourcedAggregate[ID], ID comparable](aggregate T) {
	if agg, ok := any(aggregate).(deventsourced.IEventSourcedAggregate[ID]); ok && agg != nil {
		agg.ClearUncommittedEvents()
	}
}

func (s *EventSourcedService[T, ID]) commandRetryPolicy() retry.Config {
	return s.retryConfig.toPolicyConfig(s.isConcurrencyError)
}

// wrapAggregateError 为加载聚合失败的错误补充聚合 ID 上下文。
func (s *EventSourcedService[T, ID]) wrapAggregateError(err error, aggregateID ID) error {
	var appErr *errors.AppError
	if errors.As(err, &appErr) && appErr != nil {
		return appErr.WithContext("aggregate_id", aggregateID)
	}
	return errors.Wrap(err, errors.Dependency, "load aggregate failed").
		WithContext("aggregate_id", aggregateID)
}
