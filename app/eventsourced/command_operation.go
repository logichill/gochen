package eventsourced

import (
	"context"
	"fmt"

	"gochen/app/internal/depcheck"
	"gochen/app/operation"
	"gochen/contextx"
	"gochen/errors"
)

// OperationSpecResolver 基于事件溯源命令解析 operation spec。
// 返回 nil 表示本次命令不需要 operation envelope，调用方退化为普通 ExecuteCommand 语义。
type OperationSpecResolver[ID comparable] func(ctx context.Context, cmd IEventSourcedCommand[ID]) (*operation.Spec, error)

// OperationResultResolver 在命令成功执行后构造 operation result。
// 返回 nil 时服务会使用聚合 ID 构造默认资源结果。
type OperationResultResolver[ID comparable] func(ctx context.Context, cmd IEventSourcedCommand[ID], spec *operation.Spec) (*operation.Result, error)

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
	if depcheck.IsNil(cmd) {
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
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depcheck.IsNil(cmd) {
		return nil, errors.NewCode(errors.InvalidInput, "command cannot be nil")
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
				return defaultOperationResultForCommand(cmd, spec), contextx.WrapAfterCommitError(err)
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
