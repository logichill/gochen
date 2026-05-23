package saga

import (
	"context"
	"fmt"

	gerrors "gochen/errors"
	"gochen/logging"
)

func (o *SagaOrchestrator) Execute(ctx context.Context, saga ISaga) error {
	if ctx == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "ctx is nil")
	}
	if saga == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "saga is nil")
	}

	sagaID := saga.ID()
	steps := saga.Steps()

	if len(steps) == 0 {
		return gerrors.NewCode(gerrors.InvalidInput, "saga has no steps").WithContext("saga_id", sagaID)
	}
	if err := validateSagaSteps(steps); err != nil {
		return gerrors.Wrap(err, gerrors.InvalidInput, "invalid saga steps").
			WithContext("saga_id", sagaID)
	}

	if o.lock != nil {
		release, err := o.lock.Acquire(ctx, sagaID)
		if err != nil {
			return gerrors.NewCodeWithCause(gerrors.Timeout, "failed to acquire saga lock", err).WithContext("saga_id", sagaID)
		}
		defer release()
	}

	o.logger.Info(ctx, "starting saga execution",
		logging.String("saga_id", sagaID),
		logging.Int("steps", len(steps)))

	// 创建初始状态
	state := NewSagaState(sagaID, fmt.Sprintf("%T", saga)).WithClock(o.clock)
	state.Status = SagaStatusRunning

	// Save initial state
	if o.stateStore != nil {
		if err := o.stateStore.Save(ctx, state); err != nil {
			if gerrors.Is(err, gerrors.Conflict) {
				o.logger.Warn(ctx, "saga state already exists",
					logging.String("saga_id", sagaID),
					logging.Error(err))
				return err
			}
			o.logger.Error(ctx, "failed to save saga state",
				logging.String("saga_id", sagaID),
				logging.Error(err))
			// 持久化失败属于严重错误：无法保证故障恢复语义，直接中止执行
			return gerrors.NewCodeWithCause(gerrors.Database, "failed to save saga state", err).WithContext("saga_id", sagaID)
		}
	}

	// 发布 Saga 开始事件
	o.publishEvent(ctx, EventSagaStarted, sagaID, nil)

	// 执行步骤
	for i, step := range steps {
		o.logger.Info(ctx, "executing saga step",
			logging.String("saga_id", sagaID),
			logging.Int("step_index", i),
			logging.String("step_name", step.Name))

		// 执行步骤
		if err := o.executeStep(ctx, step); err != nil {
			return o.handleStepFailure(ctx, saga, state, step, i, err)
		}

		// 标记步骤完成
		if err := o.markStepCompleted(ctx, state, sagaID, step.Name); err != nil {
			return err
		}
	}

	if err := o.completeSaga(ctx, saga, state); err != nil {
		return err
	}

	o.logger.Info(ctx, "saga execution completed",
		logging.String("saga_id", sagaID),
		logging.Int("steps", len(steps)))

	return nil
}

func (o *SagaOrchestrator) markStepCompleted(ctx context.Context, state *SagaState, sagaID string, stepName string) error {
	state.MarkStepCompleted(stepName)
	if updateErr := o.updateState(ctx, state); updateErr != nil {
		return updateErr
	}

	// 发布步骤完成事件
	o.publishEvent(ctx, EventSagaStepCompleted, sagaID, map[string]any{
		"step": stepName,
	})
	return nil
}

func (o *SagaOrchestrator) handleStepFailure(ctx context.Context, saga ISaga, state *SagaState, step *SagaStep, stepIndex int, stepErr error) error {
	sagaID := saga.ID()

	o.logger.Error(ctx, "saga step failed", logging.Error(stepErr),
		logging.String("saga_id", sagaID),
		logging.String("step_name", step.Name))

	// 标记失败
	state.MarkStepFailed(step.Name, stepErr)
	if updateErr := o.updateState(ctx, state); updateErr != nil {
		return updateErr
	}

	// 发布步骤失败事件
	o.publishEvent(ctx, EventSagaStepFailed, sagaID, map[string]any{
		"step":  step.Name,
		"error": stepErr.Error(),
	})

	// 执行补偿
	if compErr := o.compensate(ctx, saga, state, stepIndex); compErr != nil {
		var persistErr *compensationStatePersistError
		if gerrors.As(compErr, &persistErr) {
			o.logger.Error(ctx, "saga compensation completed but failed to persist compensated state", logging.Error(compErr),
				logging.String("saga_id", sagaID))

			o.notifySagaFailed(ctx, saga, stepErr)

			o.publishEvent(ctx, EventSagaCompensationCompleted, sagaID, map[string]any{
				"error":               stepErr.Error(),
				"state_persist_error": persistErr.Error(),
			})

			return gerrors.NewCodeWithCause(gerrors.Database, "saga step failed after compensation but failed to persist compensated state", compErr).
				WithContext("saga_id", sagaID).
				WithContext("step", step.Name)
		}

		o.logger.Error(ctx, "saga compensation failed", logging.Error(compErr),
			logging.String("saga_id", sagaID))

		cause := gerrors.Join(stepErr, compErr)
		// 调用失败回调
		o.notifySagaFailed(ctx, saga, gerrors.NewCodeWithCause(gerrors.Internal, "saga step failed and compensation failed", cause).
			WithContext("saga_id", sagaID).
			WithContext("step", step.Name))

		// 发布 Saga 失败事件
		o.publishEvent(ctx, EventSagaFailed, sagaID, map[string]any{
			"error":              stepErr.Error(),
			"compensation_error": compErr.Error(),
		})

		return gerrors.NewCodeWithCause(gerrors.Internal, "saga step failed and compensation failed", cause).
			WithContext("saga_id", sagaID).
			WithContext("step", step.Name)
	}

	// 调用失败回调
	o.notifySagaFailed(ctx, saga, stepErr)

	// 发布 Saga 补偿完成事件
	o.publishEvent(ctx, EventSagaCompensationCompleted, sagaID, map[string]any{
		"error": stepErr.Error(),
	})

	return gerrors.NewCodeWithCause(gerrors.Internal, "saga step failed", stepErr).
		WithContext("saga_id", sagaID).
		WithContext("step", step.Name)
}

func (o *SagaOrchestrator) completeSaga(ctx context.Context, saga ISaga, state *SagaState) error {
	sagaID := saga.ID()

	state.MarkCompleted()
	if updateErr := o.updateState(ctx, state); updateErr != nil {
		return updateErr
	}

	if err := saga.OnComplete(ctx); err != nil {
		o.logger.Warn(ctx, "saga completion callback failed", logging.Error(err),
			logging.String("saga_id", sagaID))
	}

	o.publishEvent(ctx, EventSagaCompleted, sagaID, nil)
	return nil
}

// executeStep 执行单个步骤。
func (o *SagaOrchestrator) executeStep(ctx context.Context, step *SagaStep) error {
	// 生成命令
	cmd, err := step.Command(ctx)
	if err != nil {
		return gerrors.Wrap(err, gerrors.Internal, "failed to generate command")
	}

	if cmd == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "command is nil")
	}

	if o.commandExecutor == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "command executor is nil")
	}

	// 使用显式命令执行端口执行业务步骤。
	if err := o.commandExecutor.Execute(ctx, cmd); err != nil {
		// 调用失败回调
		if step.OnFailure != nil {
			if callbackErr := step.OnFailure(ctx, step.Name, err); callbackErr != nil {
				o.logger.Warn(ctx, "step failure callback failed", logging.Error(callbackErr),
					logging.String("step", step.Name))
			}
		}
		return err
	}

	// 调用成功回调
	if step.OnSuccess != nil {
		if err := step.OnSuccess(ctx, step.Name, nil); err != nil {
			o.logger.Warn(ctx, "step success callback failed", logging.Error(err),
				logging.String("step", step.Name))
			// don't affect step success
		}
	}

	return nil
}

func (o *SagaOrchestrator) notifySagaFailed(ctx context.Context, saga ISaga, err error) {
	if saga == nil {
		return
	}
	if callbackErr := saga.OnFailed(ctx, err); callbackErr != nil {
		o.logger.Warn(ctx, "saga failure callback failed", logging.Error(callbackErr),
			logging.String("saga_id", saga.ID()))
	}
}
