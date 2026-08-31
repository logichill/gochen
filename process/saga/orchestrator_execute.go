package saga

import (
	"context"
	"encoding/base64"
	"fmt"

	"gochen/errors"
	"gochen/messaging/command"
	"gochen/observe/logging"
)

const sagaOnCompletePersistedKey = "__gochen_saga_on_complete_persisted"

func (o *SagaOrchestrator) Execute(ctx context.Context, saga ISaga) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if saga == nil {
		return errors.NewCode(errors.InvalidInput, "saga is nil")
	}

	sagaID := saga.ID()
	steps := saga.Steps()

	if len(steps) == 0 {
		return errors.NewCode(errors.InvalidInput, "saga has no steps").WithContext("saga_id", sagaID)
	}
	if err := validateSagaSteps(steps); err != nil {
		return errors.Wrap(err, errors.InvalidInput, "invalid saga steps").
			WithContext("saga_id", sagaID)
	}

	lockedCtx, release, err := o.acquireSagaLock(ctx, sagaID)
	if err != nil {
		return err
	}
	ctx = lockedCtx
	defer release()

	o.logger.Info(ctx, "starting saga execution",
		logging.String("saga_id", sagaID),
		logging.Int("steps", len(steps)))

	// 创建初始状态
	state := newSagaStateWithClock(sagaID, fmt.Sprintf("%T", saga), o.clock)
	state.MarkRunning()

	// Save initial state
	if o.stateStore != nil {
		if err := o.stateStore.Save(ctx, state); err != nil {
			if errors.Is(err, errors.Conflict) {
				o.logger.Warn(ctx, "saga state already exists",
					logging.String("saga_id", sagaID),
					logging.Error(err))
				return err
			}
			o.logger.Error(ctx, "failed to save saga state",
				logging.String("saga_id", sagaID),
				logging.Error(err))
			// 持久化失败属于严重错误：无法保证故障恢复语义，直接中止执行
			return errors.NewCodeWithCause(errors.Database, "failed to save saga state", err).WithContext("saga_id", sagaID)
		}
	}

	// 发布 Saga 开始事件
	o.publishEvent(ctx, EventSagaStarted, sagaID, nil)

	// 执行步骤
	for i, step := range steps {
		if err := o.executeStepWithLeaseGuard(ctx, saga, state, step, i); err != nil {
			return err
		}
		if err := o.markStepCompletedWithLeaseGuard(ctx, state, sagaID, step.Name); err != nil {
			return err
		}
	}

	if err := ensureSagaLeaseActive(ctx, sagaID); err != nil {
		return err
	}
	if err := o.completeSaga(ctx, saga, state); err != nil {
		return err
	}

	o.logger.Info(ctx, "saga execution completed",
		logging.String("saga_id", sagaID),
		logging.Int("steps", len(steps)))

	return nil
}

func (o *SagaOrchestrator) executeStepWithLeaseGuard(ctx context.Context, saga ISaga, state *SagaState, step *SagaStep, stepIndex int) error {
	sagaID := saga.ID()
	if err := ensureSagaLeaseActive(ctx, sagaID); err != nil {
		return err
	}
	o.logger.Info(ctx, "executing saga step",
		logging.String("saga_id", sagaID),
		logging.Int("step_index", stepIndex),
		logging.String("step_name", step.Name))

	if err := o.executeStep(ctx, sagaID, step); err != nil {
		if leaseErr := ensureSagaLeaseActive(ctx, sagaID); leaseErr != nil {
			return leaseErr
		}
		return o.handleStepFailure(ctx, saga, state, step, stepIndex, err)
	}
	return ensureSagaLeaseActive(ctx, sagaID)
}

func (o *SagaOrchestrator) markStepCompletedWithLeaseGuard(ctx context.Context, state *SagaState, sagaID string, stepName string) error {
	if err := o.markStepCompleted(ctx, state, sagaID, stepName); err != nil {
		return err
	}
	return ensureSagaLeaseActive(ctx, sagaID)
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
		"step":       step.Name,
		"error":      sagaPublicErrorMessage(stepErr),
		"error_code": string(sagaErrorCode(stepErr)),
	})

	if !hasCompensationBefore(saga.Steps(), stepIndex) {
		o.notifySagaFailed(ctx, saga, stepErr)
		o.publishEvent(ctx, EventSagaFailed, sagaID, map[string]any{
			"error":      sagaPublicErrorMessage(stepErr),
			"error_code": string(sagaErrorCode(stepErr)),
		})
		return errors.NewCodeWithCause(errors.Internal, "saga step failed", stepErr).
			WithContext("saga_id", sagaID).
			WithContext("step", step.Name)
	}

	// 执行补偿
	if compErr := o.compensate(ctx, saga, state, stepIndex); compErr != nil {
		if leaseErr := ensureSagaLeaseActive(ctx, sagaID); leaseErr != nil {
			return leaseErr
		}
		var persistErr *compensationStatePersistError
		if errors.As(compErr, &persistErr) {
			o.logger.Error(ctx, "saga compensation completed but failed to persist compensated state", logging.Error(compErr),
				logging.String("saga_id", sagaID))

			o.notifySagaFailed(ctx, saga, stepErr)

			o.publishEvent(ctx, EventSagaCompensationCompleted, sagaID, map[string]any{
				"error":                    sagaPublicErrorMessage(stepErr),
				"error_code":               string(sagaErrorCode(stepErr)),
				"state_persist_error":      sagaPublicErrorMessage(persistErr),
				"state_persist_error_code": string(sagaErrorCode(persistErr)),
			})

			return errors.NewCodeWithCause(errors.Database, "saga step failed after compensation but failed to persist compensated state", compErr).
				WithContext("saga_id", sagaID).
				WithContext("step", step.Name)
		}

		o.logger.Error(ctx, "saga compensation failed", logging.Error(compErr),
			logging.String("saga_id", sagaID))

		cause := errors.Join(stepErr, compErr)
		failedErr := errors.NewCodeWithCause(errors.Internal, "saga step failed and compensation failed", cause).
			WithContext("saga_id", sagaID).
			WithContext("step", step.Name)
		state.MarkFailed(failedErr)
		if updateErr := o.updateState(ctx, state); updateErr != nil {
			return updateErr
		}

		// 调用失败回调
		o.notifySagaFailed(ctx, saga, failedErr)

		// 发布 Saga 失败事件
		o.publishEvent(ctx, EventSagaFailed, sagaID, map[string]any{
			"error":                   sagaPublicErrorMessage(stepErr),
			"error_code":              string(sagaErrorCode(stepErr)),
			"compensation_error":      sagaPublicErrorMessage(compErr),
			"compensation_error_code": string(sagaErrorCode(compErr)),
		})

		return failedErr
	}

	// 调用失败回调
	o.notifySagaFailed(ctx, saga, stepErr)

	// 发布 Saga 补偿完成事件
	o.publishEvent(ctx, EventSagaCompensationCompleted, sagaID, map[string]any{
		"error":      sagaPublicErrorMessage(stepErr),
		"error_code": string(sagaErrorCode(stepErr)),
	})

	return errors.NewCodeWithCause(errors.Internal, "saga step failed", stepErr).
		WithContext("saga_id", sagaID).
		WithContext("step", step.Name)
}

func hasCompensationBefore(steps []*SagaStep, failedStepIndex int) bool {
	for i := failedStepIndex - 1; i >= 0; i-- {
		if steps[i] != nil && steps[i].HasCompensation() {
			return true
		}
	}
	return false
}

func (o *SagaOrchestrator) completeSaga(ctx context.Context, saga ISaga, state *SagaState) error {
	sagaID := saga.ID()

	if !sagaOnCompletePersisted(state) {
		if err := ensureSagaLeaseActive(ctx, sagaID); err != nil {
			return err
		}
		completeErr := saga.OnComplete(ctx)
		if completeErr != nil {
			o.logger.Error(ctx, "saga completion callback failed", logging.Error(completeErr),
				logging.String("saga_id", sagaID))

			// 所有业务步骤已成功并持久化，仅完成回调失败：标记为可恢复的 PendingCompletion
			// 而非终态 Failed，使 Resume 能重新进入 completeSaga 重试 OnComplete（要求 OnComplete 幂等）。
			// 此处不调用 OnFailed —— saga 并未失败，避免触发失败清理等副作用。
			state.MarkPendingCompletion(completeErr)
			if updateErr := o.updateState(ctx, state); updateErr != nil {
				return updateErr
			}
			if err := ensureSagaLeaseActive(ctx, sagaID); err != nil {
				return err
			}
			o.publishEvent(ctx, EventSagaCompletionFailed, sagaID, map[string]any{
				"error":      sagaPublicErrorMessage(completeErr),
				"error_code": string(sagaErrorCode(completeErr)),
			})

			return errors.NewCodeWithCause(errors.Internal, "saga completion callback failed", completeErr).
				WithContext("saga_id", sagaID)
		}

		state.SetData(sagaOnCompletePersistedKey, true)
		if updateErr := o.updateState(ctx, state); updateErr != nil {
			return updateErr
		}
		if err := ensureSagaLeaseActive(ctx, sagaID); err != nil {
			return err
		}
	}
	state.MarkCompleted()
	if updateErr := o.updateState(ctx, state); updateErr != nil {
		return updateErr
	}
	if err := ensureSagaLeaseActive(ctx, sagaID); err != nil {
		return err
	}

	o.publishEvent(ctx, EventSagaCompleted, sagaID, nil)
	return nil
}

func sagaOnCompletePersisted(state *SagaState) bool {
	if state == nil {
		return false
	}
	value, ok := state.GetData(sagaOnCompletePersistedKey)
	if !ok {
		return false
	}
	persisted, ok := value.(bool)
	return ok && persisted
}

func sagaErrorCode(err error) errors.ErrorCode {
	code := errors.Code(err)
	if code == "" {
		return errors.Internal
	}
	return code
}

func sagaPublicErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var appErr *errors.AppError
	if errors.As(err, &appErr) && appErr != nil {
		return appErr.Message()
	}
	if message := errors.PublicMessage(sagaErrorCode(err)); message != "" {
		return message
	}
	return "saga execution failed"
}

// executeStep 执行单个步骤。
func (o *SagaOrchestrator) executeStep(ctx context.Context, sagaID string, step *SagaStep) error {
	// 生成命令
	cmd, err := step.Command(ctx)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to generate command")
	}

	if cmd == nil {
		return errors.NewCode(errors.InvalidInput, "command is nil")
	}

	if o.commandExecutor == nil {
		return errors.NewCode(errors.InvalidInput, "command executor is nil")
	}

	ensureCommandIdempotencyKey(cmd, sagaStepIdempotencyKey(sagaID, step.Name))

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

func ensureCommandIdempotencyKey(cmd *command.Command, key string) {
	if cmd == nil || key == "" {
		return
	}
	if value, ok := cmd.GetMetadata().GetString(command.MetadataIdempotencyKey); ok && value != "" {
		return
	}
	cmd.WithMetadata(command.MetadataIdempotencyKey, key)
}

func sagaStepIdempotencyKey(sagaID, stepName string) string {
	return "saga.step." + encodeSagaIdempotencyPart(sagaID) + "." + encodeSagaIdempotencyPart(stepName)
}

func sagaCompensationIdempotencyKey(sagaID, stepName string) string {
	return "saga.compensation." + encodeSagaIdempotencyPart(sagaID) + "." + encodeSagaIdempotencyPart(stepName)
}

func encodeSagaIdempotencyPart(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
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
