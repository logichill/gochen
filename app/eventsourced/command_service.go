package eventsourced

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"gochen/app/internal/commandflow"
	"gochen/app/operation"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/observe/logging"
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

// OperationSpecResolver 基于事件溯源命令解析 operation spec。
// 返回 nil 表示本次命令不需要 operation envelope，调用方退化为普通 ExecuteCommand 语义。
type OperationSpecResolver[ID comparable] func(ctx context.Context, cmd IEventSourcedCommand[ID]) (*operation.Spec, error)

// OperationResultResolver 在命令成功执行后构造 operation result。
// 返回 nil 时服务会使用聚合 ID 构造默认资源结果。
type OperationResultResolver[ID comparable] func(ctx context.Context, cmd IEventSourcedCommand[ID], spec *operation.Spec) (*operation.Result, error)

// EventSourcedServiceOptions 事件溯源命令服务配置（应用层）。
type EventSourcedServiceOptions[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	Logger        logging.ILogger
	CommandHooks  []IEventSourcedCommandHook[T, ID]
	CommandTracer ICommandTracer

	// OperationRunner 为 ExecuteCommandWithOperation 提供 operation envelope 包装器（可选）。
	//
	// 未配置时 inline operation 使用 operation.DefaultRunner()；tracked operation 必须显式配置
	// 带持久化 store 的 runner，避免返回不可查询的 operation id。
	OperationRunner operation.IRunner

	// OperationSpecResolver 为 ExecuteCommandWithResolvedOperation 按命令解析 operation spec（可选）。
	OperationSpecResolver OperationSpecResolver[ID]

	// OperationResultResolver 为 ExecuteCommandWithResolvedOperation 构造业务 result（可选）。
	//
	// 未配置或返回 nil 时，默认使用命令聚合 ID 补齐 Resource.ID。
	OperationResultResolver OperationResultResolver[ID]

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
	// - 当加载聚合失败时，agg 为 repository 返回值，可能是工厂创建的空实例，也可能是 T 的零值，hook 实现必须能处理该情况；
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
// - 当加载聚合失败时，agg 为 repository 返回值，可能是工厂创建的空实例，也可能是 T 的零值，hook 实现必须能处理该情况；
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
		service.hooks = filterCommandHooks(opts.CommandHooks)
		service.tracer = opts.CommandTracer
		service.logger = opts.Logger
		service.operationRunner = opts.OperationRunner
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
// 该方法是并发安全的，可在运行时调用。
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

func filterCommandHooks[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	hooks []IEventSourcedCommandHook[T, ID],
) []IEventSourcedCommandHook[T, ID] {
	if len(hooks) == 0 {
		return nil
	}
	filtered := make([]IEventSourcedCommandHook[T, ID], 0, len(hooks))
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		filtered = append(filtered, hook)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// ExecuteCommand 完成一次“加载聚合 -> 执行业务 -> 保存聚合”的命令执行流程。
func (s *EventSourcedService[T, ID]) ExecuteCommand(ctx context.Context, cmd IEventSourcedCommand[ID]) error {
	_, err := s.executeCommand(ctx, cmd)
	return err
}

// ExecuteCommandWithOperation 在 operation envelope 中执行一次事件溯源命令。
//
// 命令执行、hook、重试与追踪语义与 ExecuteCommand 保持一致；operation 的
// ID、状态与 tracked/inline 差异由配置的 OperationRunner 统一处理。
func (s *EventSourcedService[T, ID]) ExecuteCommandWithOperation(ctx context.Context, cmd IEventSourcedCommand[ID], spec *operation.Spec) (*operation.Result, error) {
	return s.executeCommandWithOperation(ctx, cmd, spec, nil)
}

// ExecuteCommandWithResolvedOperation 使用配置的 resolver 解析 spec 后执行事件溯源命令。
//
// 当 OperationSpecResolver 未配置或返回 nil 时，该方法退化为 ExecuteCommand，并返回 nil result。
func (s *EventSourcedService[T, ID]) ExecuteCommandWithResolvedOperation(ctx context.Context, cmd IEventSourcedCommand[ID]) (*operation.Result, error) {
	if s == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event sourced service is nil")
	}
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if cmd == nil {
		return nil, errors.NewCode(errors.InvalidInput, "command cannot be nil")
	}
	if s.operationSpecResolver == nil {
		if err := s.ExecuteCommand(ctx, cmd); err != nil {
			return nil, err
		}
		return nil, nil
	}
	spec, err := s.operationSpecResolver(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if spec == nil {
		if err := s.ExecuteCommand(ctx, cmd); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return s.executeCommandWithOperation(ctx, cmd, spec, s.operationResultResolver)
}

func (s *EventSourcedService[T, ID]) executeCommandWithOperation(
	ctx context.Context,
	cmd IEventSourcedCommand[ID],
	spec *operation.Spec,
	resultResolver OperationResultResolver[ID],
) (*operation.Result, error) {
	if s == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event sourced service is nil")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	runner := s.operationRunner
	if runner == nil {
		if spec.Mode == operation.ModeTracked {
			return nil, errors.NewCode(errors.FailedPrecondition, "operation runner is required for tracked event sourced command")
		}
		runner = operation.DefaultRunner()
	}
	return runner.Execute(ctx, spec, func(opCtx context.Context) (*operation.Result, error) {
		if _, err := s.executeCommand(opCtx, cmd); err != nil {
			return nil, err
		}
		if resultResolver != nil {
			result, err := resultResolver(opCtx, cmd, spec)
			if err != nil {
				return nil, err
			}
			if result != nil {
				return result, nil
			}
		}
		return defaultOperationResultForCommand(cmd, spec), nil
	})
}

func defaultOperationResultForCommand[ID comparable](cmd IEventSourcedCommand[ID], spec *operation.Spec) *operation.Result {
	return operation.MergeResult(nil, spec, &operation.Resource{ID: fmt.Sprint(cmd.AggregateID())})
}

func (s *EventSourcedService[T, ID]) executeCommand(ctx context.Context, cmd IEventSourcedCommand[ID]) (T, error) {
	var zero T
	if s == nil {
		return zero, errors.NewCode(errors.InvalidInput, "event sourced service is nil")
	}
	if ctx == nil {
		return zero, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if cmd == nil {
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
