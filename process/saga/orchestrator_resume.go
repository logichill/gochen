package saga

import (
	"context"

	"gochen/errors"
	"gochen/observe/logging"
)

func (o *SagaOrchestrator) Resume(ctx context.Context, saga ISaga, state *SagaState) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if saga == nil {
		return errors.NewCode(errors.InvalidInput, "saga is nil")
	}

	sagaID := saga.ID()
	steps := saga.Steps()

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

	if err := validateResumeState(sagaID, state, steps); err != nil {
		return err
	}
	state = state.Clone()
	state = state.WithClock(o.clock)

	o.logger.Info(ctx, "resuming saga execution",
		logging.String("saga_id", sagaID),
		logging.Int("current_step", state.CurrentStep))

	// 发布恢复事件
	o.publishEvent(ctx, EventSagaResumed, sagaID, map[string]any{
		"current_step": state.CurrentStep,
		"status":       string(state.Status),
	})

	// 从当前步骤继续执行
	for i := state.CurrentStep; i < len(steps); i++ {
		step := steps[i]

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

	o.logger.Info(ctx, "saga resume execution completed",
		logging.String("saga_id", sagaID))

	return nil
}

func validateResumeState(sagaID string, state *SagaState, steps []*SagaStep) error {
	if state == nil {
		return errors.NewCode(errors.InvalidInput, "saga state is nil").
			WithContext("saga_id", sagaID)
	}
	if state.SagaID == "" {
		return errors.NewCode(errors.InvalidInput, "saga state id is empty").
			WithContext("saga_id", sagaID)
	}
	if state.SagaID != sagaID {
		return errors.NewCode(errors.InvalidInput, "saga state id does not match saga").
			WithContext("saga_id", sagaID).
			WithContext("state_saga_id", state.SagaID)
	}
	if state.IsCompleted() {
		return errors.NewCode(errors.Conflict, "saga already completed").WithContext("saga_id", sagaID)
	}
	if state.Status == SagaStatusPending {
		return errors.NewCode(errors.FailedPrecondition, "pending saga must be executed before resume").
			WithContext("saga_id", sagaID)
	}
	if state.IsFailed() || state.IsCompensated() {
		return errors.NewCode(errors.Conflict, "saga already failed").WithContext("saga_id", sagaID)
	}
	if state.IsCompensating() {
		return errors.NewCode(errors.Conflict, "saga is compensating and cannot be resumed directly").
			WithContext("saga_id", sagaID)
	}
	if state.IsPendingCompletion() && state.CurrentStep != len(steps) {
		return errors.NewCode(errors.InvalidInput, "pending completion saga must have completed all steps").
			WithContext("saga_id", sagaID).
			WithContext("current_step", state.CurrentStep).
			WithContext("steps", len(steps))
	}
	if state.CurrentStep < 0 || state.CurrentStep > len(steps) {
		return errors.NewCode(errors.InvalidInput, "saga current step is out of range").
			WithContext("saga_id", sagaID).
			WithContext("current_step", state.CurrentStep).
			WithContext("steps", len(steps))
	}
	if len(state.CompletedSteps) != state.CurrentStep {
		return errors.NewCode(errors.InvalidInput, "saga completed steps do not match current step").
			WithContext("saga_id", sagaID).
			WithContext("current_step", state.CurrentStep).
			WithContext("completed_steps", len(state.CompletedSteps))
	}
	for i, completedStep := range state.CompletedSteps {
		expectedStep := steps[i].Name
		if completedStep != expectedStep {
			return errors.NewCode(errors.InvalidInput, "saga completed steps are inconsistent with workflow definition").
				WithContext("saga_id", sagaID).
				WithContext("current_step", state.CurrentStep).
				WithContext("step_index", i).
				WithContext("expected_step", expectedStep).
				WithContext("completed_step", completedStep)
		}
	}
	return nil
}
