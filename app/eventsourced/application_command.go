package eventsourced

import (
	"context"

	"gochen/app/internal/commandflow"
	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/spec"
	"gochen/errors"
)

// ExecOption 配置一次命令执行的审计元数据。
type ExecOption = execution.ExecOption

// WithReason 设置本次命令的审计原因。
func WithReason(reason string) ExecOption { return execution.WithReason(reason) }

// commandContext 解析审计执行者，并把 operator 与事件审计写入 ctx。
func (a *Application[T, ID]) commandContext(ctx context.Context, action string, opts []ExecOption) (context.Context, error) {
	actor, err := execution.ResolveActor(ctx, opts, false)
	if err != nil {
		return nil, err
	}
	if actor.Operator != "" {
		ctx, err = contextx.WithOperator(ctx, actor.Operator)
		if err != nil {
			return nil, err
		}
	}
	return contextx.WithEventAudit(ctx, contextx.EventAudit{
		Operator:      actor.Operator,
		Initiator:     actor.Initiator,
		Action:        action,
		Reason:        actor.Reason,
		CorrelationID: actor.CorrelationID,
	}), nil
}

// executeAttempt 完成一次回放 → 匹配 → 命令 → 核对 → 保存。
func (a *Application[T, ID]) executeAttempt(
	ctx context.Context,
	id ID,
	spec spec.Spec,
	assignments []auth.Assignment,
	fn func(ctx context.Context, aggregate T) error,
) (T, error) {
	var zero T
	aggregate, err := a.repo.Get(ctx, id)
	if err != nil {
		return zero, wrapAggregateError(err, id)
	}
	if err := a.matchAggregate(spec, aggregate, errors.NotFound); err != nil {
		return zero, err
	}
	protected, err := a.execution.CaptureProtected(aggregate)
	if err != nil {
		return zero, err
	}
	if err := fn(ctx, aggregate); err != nil {
		aggregate.ClearUncommittedEvents()
		return zero, err
	}
	if err := a.execution.CheckProtected(protected, aggregate); err != nil {
		aggregate.ClearUncommittedEvents()
		return zero, err
	}
	if err := a.validateAfterCommand(aggregate, assignments); err != nil {
		aggregate.ClearUncommittedEvents()
		return zero, err
	}
	if err := a.matchAggregate(spec, aggregate, errors.Forbidden); err != nil {
		aggregate.ClearUncommittedEvents()
		return zero, err
	}
	if err := a.repo.Save(ctx, aggregate); err != nil {
		if contextx.IsAfterCommitError(err) {
			return aggregate, err
		}
		aggregate.ClearUncommittedEvents()
		return zero, err
	}
	return aggregate, nil
}

func commandflowRun[T any](
	ctx context.Context,
	cfg *RetryConfig,
	isConcurrencyError IsConcurrencyError,
	attempt func(ctx context.Context) (T, error),
) (T, error) {
	result, err := commandflow.Run(ctx, commandflow.Plan[T]{
		Attempt: func(attemptCtx context.Context, _ int) (T, error) {
			return attempt(attemptCtx)
		},
		RetryConfig: cfg.toPolicyConfig(isConcurrencyError),
		WrapFinalError: func(finalErr error, _ int) error {
			if finalErr == nil || contextx.IsAfterCommitError(finalErr) || cfg == nil || cfg.MaxRetries <= 0 || !isConcurrencyError(finalErr) {
				return finalErr
			}
			return &RetryExhaustedError{Cause: finalErr, MaxRetries: cfg.MaxRetries}
		},
	})
	if err != nil && !contextx.IsAfterCommitError(err) {
		var zero T
		return zero, err
	}
	return result.State, err
}

// wrapAggregateError 为回放失败的错误补充聚合 ID 上下文。
func wrapAggregateError(err error, id any) error {
	var appErr *errors.AppError
	if errors.As(err, &appErr) && appErr != nil {
		return appErr.WithContext("aggregate_id", id)
	}
	return errors.Wrap(err, errors.Dependency, "load aggregate failed").
		WithContext("aggregate_id", id)
}
