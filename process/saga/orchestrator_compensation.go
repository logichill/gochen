package saga

import (
	"context"

	gerrors "gochen/errors"
	"gochen/logging"
)

type compensationStatePersistError struct {
	cause error
}

func (e *compensationStatePersistError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *compensationStatePersistError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// compensate 执行补偿。
func (o *SagaOrchestrator) compensate(ctx context.Context, saga ISaga, state *SagaState, failedStepIndex int) error {
	sagaID := saga.ID()
	steps := saga.Steps()

	o.logger.Info(ctx, "starting saga compensation",
		logging.String("saga_id", sagaID),
		logging.Int("failed_step_index", failedStepIndex))

	// 标记补偿中
	state.MarkCompensating()
	if updateErr := o.updateState(ctx, state); updateErr != nil {
		return updateErr
	}

	// 发布补偿开始事件
	o.publishEvent(ctx, EventSagaCompensationStarted, sagaID, nil)

	// 倒序执行补偿（从失败步骤的前一个开始）
	for i := failedStepIndex - 1; i >= 0; i-- {
		step := steps[i]

		// 如果没有补偿命令，跳过
		if !step.HasCompensation() {
			o.logger.Info(ctx, "step has no compensation, skipping",
				logging.String("saga_id", sagaID),
				logging.String("step", step.Name))
			continue
		}

		o.logger.Info(ctx, "executing compensation",
			logging.String("saga_id", sagaID),
			logging.Int("step_index", i),
			logging.String("step", step.Name))

		// 生成补偿命令
		compCmd, err := step.Compensation(ctx)
		if err != nil {
			o.logger.Error(ctx, "failed to generate compensation command", logging.Error(err),
				logging.String("saga_id", sagaID),
				logging.String("step", step.Name))
			return gerrors.NewCodeWithCause(gerrors.Internal, "failed to generate compensation command", err).WithContext("saga_id", sagaID).WithContext("step", step.Name)
		}

		if compCmd == nil {
			o.logger.Error(ctx, "compensation command is nil",
				logging.String("saga_id", sagaID),
				logging.String("step", step.Name))
			return gerrors.NewCode(gerrors.Internal, "compensation command is nil").WithContext("saga_id", sagaID).WithContext("step", step.Name)
		}

		if o.commandExecutor == nil {
			return gerrors.NewCode(gerrors.InvalidInput, "command executor is nil").
				WithContext("saga_id", sagaID).
				WithContext("step", step.Name)
		}

		// 执行补偿命令
		if err := o.commandExecutor.Execute(ctx, compCmd); err != nil {
			o.logger.Error(ctx, "failed to execute compensation command", logging.Error(err),
				logging.String("saga_id", sagaID),
				logging.String("step", step.Name))

			// 发布补偿失败事件
			o.publishEvent(ctx, EventSagaCompensationStepFailed, sagaID, map[string]any{
				"step":  step.Name,
				"error": err.Error(),
			})

			return gerrors.NewCodeWithCause(gerrors.Internal, "compensation execution failed", err).WithContext("saga_id", sagaID).WithContext("step", step.Name)
		}

		// 发布补偿步骤完成事件
		o.publishEvent(ctx, EventSagaCompensationStepCompleted, sagaID, map[string]any{
			"step": step.Name,
		})
	}

	// 标记补偿完成
	state.MarkCompensated()
	if updateErr := o.updateState(ctx, state); updateErr != nil {
		return &compensationStatePersistError{cause: updateErr}
	}

	o.logger.Info(ctx, "saga compensation completed",
		logging.String("saga_id", sagaID))

	return nil
}
