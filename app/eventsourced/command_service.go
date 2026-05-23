package eventsourced

import (
	"context"
	"reflect"
	"time"

	"gochen/app/internal/commandflow"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/logging"
	"gochen/policy/retry"
)

// IEventSourcedCommand 事件溯源命令接口（应用层）。
//
// 命令需要提供聚合 ID，用于定位目标聚合根。
//
// 类型参数：
//   - ID: 聚合根 ID 类型，必须是可比较类型（如 int64、string、uuid.UUID 等）
type IEventSourcedCommand[ID comparable] interface {
	AggregateID() ID
}

// EventSourcedCommandHandler 命令处理器函数类型。
// 处理器接收命令与聚合实例，并在聚合上执行业务逻辑。
type EventSourcedCommandHandler[T deventsourced.IEventSourcedAggregate[ID], ID comparable] func(ctx context.Context, cmd IEventSourcedCommand[ID], aggregate T) error

// EventSourcedServiceOptions 事件溯源命令服务配置（应用层）。
type EventSourcedServiceOptions[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	Logger        logging.ILogger
	CommandHooks  []IEventSourcedCommandHook[T, ID]
	CommandTracer ICommandTracer

	// ConcurrencyRetry 配置“保存阶段遇到并发冲突（errors.Concurrency）”时的自动重试（可选）。
	//
	// 语义：
	// - 重试发生在 ExecuteCommand 内部：会重新加载聚合并重新执行 handler（而不是重放旧未提交事件）；
	// - 因此要求 handler 具备“可重入/幂等”特性：不要在 handler 内产生不可回滚的外部副作用。
	ConcurrencyRetry *RetryConfig

	// IsConcurrencyError 自定义“是否为并发冲突错误”的判断函数（可选）。
	//
	// 默认使用 errors.Is(err, errors.Concurrency)。
	IsConcurrencyError IsConcurrencyError
}

// IEventSourcedCommandHook 命令执行钩子接口。
// 可用于统计、审计、校验等横切逻辑。
type IEventSourcedCommandHook[T deventsourced.IEventSourcedAggregate[ID], ID comparable] interface {
	BeforeExecute(ctx context.Context, cmd IEventSourcedCommand[ID], agg T) error

	// AfterExecute 在一次命令执行尝试结束后调用。
	//
	// 注意：
	// - 参数 err 表示“本次尝试的最终错误”（可能来自加载聚合、BeforeExecute、handler 或保存阶段）；
	// - 当加载聚合失败时，agg 为 T 的零值（常见为 nil），hook 实现必须能处理该情况；
	// - 当某个 hook 的 BeforeExecute 返回错误时，后续 hook 的 BeforeExecute 不会再被调用，但它们仍会收到 AfterExecute；
	// - 当启用并发重试（ConcurrencyRetry）时，一个命令可能会触发多次尝试，因此该 hook 可能被调用多次。
	AfterExecute(ctx context.Context, cmd IEventSourcedCommand[ID], agg T, err error) error
}

// IEventSourcedCommandFinalizeHook 命令执行“最终一次”钩子接口（可选）。
//
// 用途：用于 metrics/审计/收尾等“只希望每个命令执行调用一次”的逻辑。
//
// 注意：
// - 每次 ExecuteCommand 只会调用一次（无论成功/失败/加载失败/BeforeExecute 失败/重试耗尽）；
// - 参数 err 表示“本次命令执行的最终错误”（成功则为 nil）；
// - 当加载聚合失败时，agg 为 T 的零值（常见为 nil），hook 实现必须能处理该情况；
// - attempts 表示“实际尝试次数”（包含首次执行尝试；重试耗尽时通常为 MaxRetries+1）。
type IEventSourcedCommandFinalizeHook[T deventsourced.IEventSourcedAggregate[ID], ID comparable] interface {
	AfterFinalize(ctx context.Context, cmd IEventSourcedCommand[ID], agg T, err error, attempts int) error
}

// ICommandTracer 提供命令执行过程的耗时与错误追踪。
type ICommandTracer interface {
	Trace(ctx context.Context, commandName string, elapsed time.Duration, err error)
}

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
	handlers   map[reflect.Type]EventSourcedCommandHandler[T, ID]
	logger     logging.ILogger
	hooks      []IEventSourcedCommandHook[T, ID]
	tracer     ICommandTracer

	retryConfig        *RetryConfig
	isConcurrencyError IsConcurrencyError
}

// NewEventSourcedService 创建一个事件溯源命令执行服务。
func NewEventSourcedService[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	repository deventsourced.IEventSourcedRepository[T, ID],
	opts *EventSourcedServiceOptions[T, ID],
) (*EventSourcedService[T, ID], error) {
	if repository == nil {
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
		service.hooks = opts.CommandHooks
		service.tracer = opts.CommandTracer
		service.logger = opts.Logger
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
func (s *EventSourcedService[T, ID]) RegisterCommandHandler(prototype IEventSourcedCommand[ID], handler EventSourcedCommandHandler[T, ID]) error {
	if prototype == nil {
		return errors.NewCode(errors.InvalidInput, "command prototype cannot be nil")
	}
	cmdType := reflect.TypeOf(prototype)
	if cmdType.Kind() != reflect.Ptr {
		return errors.NewCode(errors.InvalidInput, "command prototype must be pointer type").
			WithContext("command_type", cmdType.String())
	}
	s.handlers[cmdType] = handler
	return nil
}

// ExecuteCommand 完成一次“加载聚合 -> 执行业务 -> 保存聚合”的命令执行流程。
func (s *EventSourcedService[T, ID]) ExecuteCommand(ctx context.Context, cmd IEventSourcedCommand[ID]) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if cmd == nil {
		return errors.NewCode(errors.InvalidInput, "command cannot be nil")
	}
	cmdType := reflect.TypeOf(cmd)
	handler, exists := s.handlers[cmdType]
	if !exists {
		return errors.NewCode(errors.NotFound, "command handler not found").
			WithContext("command_type", cmdType.String())
	}

	aggregateID := cmd.AggregateID()
	commandName := cmdType.String()
	_, err := commandflow.Run(ctx, commandflow.Plan[T]{
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
			if finalErr == nil || s.retryConfig == nil || s.retryConfig.MaxRetries <= 0 || !s.isConcurrencyError(finalErr) {
				return finalErr
			}
			return &RetryExhaustedError{Cause: finalErr, MaxRetries: s.retryConfig.MaxRetries}
		},
		Trace: func(traceCtx context.Context, attempts int, elapsed time.Duration, finalErr error) {
			_ = attempts
			s.trace(traceCtx, commandName, elapsed, finalErr)
		},
	})
	return err
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
	return aggregate, finalErr
}

func (s *EventSourcedService[T, ID]) commandRetryPolicy() retry.Config {
	return s.retryConfig.toPolicyConfig(s.isConcurrencyError)
}

func (s *EventSourcedService[T, ID]) runAfterFinalizeHooks(
	ctx context.Context,
	cmd IEventSourcedCommand[ID],
	commandName string,
	aggregate T,
	err error,
	attempts int,
) {
	for _, hook := range s.hooks {
		finalizeHook, ok := hook.(IEventSourcedCommandFinalizeHook[T, ID])
		if !ok {
			continue
		}
		if hookErr := finalizeHook.AfterFinalize(ctx, cmd, aggregate, err, attempts); hookErr != nil {
			if s.logger != nil {
				s.logger.Warn(ctx, "after finalize hook failed",
					logging.Error(hookErr),
					logging.String("command", commandName))
			}
		}
	}
}

// runBeforeExecuteHooks 依次执行所有 BeforeExecute 钩子。
func (s *EventSourcedService[T, ID]) runBeforeExecuteHooks(ctx context.Context, cmd IEventSourcedCommand[ID], aggregate T) error {
	for _, hook := range s.hooks {
		if err := hook.BeforeExecute(ctx, cmd, aggregate); err != nil {
			var appErr *errors.AppError
			if errors.As(err, &appErr) && appErr != nil {
				return appErr
			}
			return errors.Wrap(err, errors.Internal, "before execute hook failed")
		}
	}
	return nil
}

// runAfterExecuteHooks 依次执行所有 AfterExecute 钩子。
func (s *EventSourcedService[T, ID]) runAfterExecuteHooks(ctx context.Context, cmd IEventSourcedCommand[ID], commandName string, aggregate T, err error) {
	for _, hook := range s.hooks {
		if hookErr := hook.AfterExecute(ctx, cmd, aggregate, err); hookErr != nil {
			if s.logger != nil {
				s.logger.Warn(ctx, "after execute hook failed",
					logging.Error(hookErr),
					logging.String("command", commandName))
			}
		}
	}
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

// trace 将一次命令执行的耗时与结果上报给 tracer。
func (s *EventSourcedService[T, ID]) trace(ctx context.Context, commandName string, elapsed time.Duration, execErr error) {
	if s.tracer != nil {
		s.tracer.Trace(ctx, commandName, elapsed, execErr)
	}
}
