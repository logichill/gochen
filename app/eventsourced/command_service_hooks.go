package eventsourced

import (
	"context"
	"time"

	"gochen/app/internal/depcheck"
	deventsourced "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/observe/logging"
)

func filterCommandHooks[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	hooks []IEventSourcedCommandHook[T, ID],
) []IEventSourcedCommandHook[T, ID] {
	if len(hooks) == 0 {
		return nil
	}
	filtered := make([]IEventSourcedCommandHook[T, ID], 0, len(hooks))
	for _, hook := range hooks {
		if depcheck.IsNil(hook) {
			continue
		}
		filtered = append(filtered, hook)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
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

// trace 将一次命令执行的耗时与结果上报给 tracer。
func (s *EventSourcedService[T, ID]) trace(ctx context.Context, commandName string, elapsed time.Duration, execErr error) {
	if s.tracer != nil {
		s.tracer.Trace(ctx, commandName, elapsed, execErr)
	}
}
