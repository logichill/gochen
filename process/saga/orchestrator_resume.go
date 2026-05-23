package saga

import (
	"context"

	gerrors "gochen/errors"
	"gochen/logging"
)

func (o *SagaOrchestrator) Resume(ctx context.Context, saga ISaga, state *SagaState) error {
	if ctx == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "ctx is nil")
	}
	if saga == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "saga is nil")
	}

	sagaID := saga.ID()
	state = state.WithClock(o.clock)
	steps := saga.Steps()

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

	if err := validateResumeState(sagaID, state, steps); err != nil {
		return err
	}

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

		if err := o.executeStep(ctx, step); err != nil {
			return o.handleStepFailure(ctx, saga, state, step, i, err)
		}

		if err := o.markStepCompleted(ctx, state, sagaID, step.Name); err != nil {
			return err
		}
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
		return gerrors.NewCode(gerrors.InvalidInput, "saga state is nil").
			WithContext("saga_id", sagaID)
	}
	if state.SagaID == "" {
		return gerrors.NewCode(gerrors.InvalidInput, "saga state id is empty").
			WithContext("saga_id", sagaID)
	}
	if state.SagaID != sagaID {
		return gerrors.NewCode(gerrors.InvalidInput, "saga state id does not match saga").
			WithContext("saga_id", sagaID).
			WithContext("state_saga_id", state.SagaID)
	}
	if state.IsCompleted() {
		return gerrors.NewCode(gerrors.Conflict, "saga already completed").WithContext("saga_id", sagaID)
	}
	if state.IsFailed() || state.IsCompensated() {
		return gerrors.NewCode(gerrors.Conflict, "saga already failed").WithContext("saga_id", sagaID)
	}
	if state.IsCompensating() {
		return gerrors.NewCode(gerrors.Conflict, "saga is compensating and cannot be resumed directly").
			WithContext("saga_id", sagaID)
	}
	if state.CurrentStep < 0 || state.CurrentStep > len(steps) {
		return gerrors.NewCode(gerrors.InvalidInput, "saga current step is out of range").
			WithContext("saga_id", sagaID).
			WithContext("current_step", state.CurrentStep).
			WithContext("steps", len(steps))
	}
	if len(state.CompletedSteps) != state.CurrentStep {
		return gerrors.NewCode(gerrors.InvalidInput, "saga completed steps do not match current step").
			WithContext("saga_id", sagaID).
			WithContext("current_step", state.CurrentStep).
			WithContext("completed_steps", len(state.CompletedSteps))
	}
	for i, completedStep := range state.CompletedSteps {
		expectedStep := steps[i].Name
		if completedStep != expectedStep {
			return gerrors.NewCode(gerrors.InvalidInput, "saga completed steps are inconsistent with workflow definition").
				WithContext("saga_id", sagaID).
				WithContext("current_step", state.CurrentStep).
				WithContext("step_index", i).
				WithContext("expected_step", expectedStep).
				WithContext("completed_step", completedStep)
		}
	}
	return nil
}
