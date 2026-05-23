package saga

import (
	"context"

	gerrors "gochen/errors"
	"gochen/logging"
)

// updateState 更新状态。
func (o *SagaOrchestrator) updateState(ctx context.Context, state *SagaState) error {
	if o.stateStore == nil {
		return nil
	}

	if err := o.stateStore.Update(ctx, state); err != nil {
		o.logger.Error(ctx, "failed to update saga state",
			logging.String("saga_id", state.SagaID),
			logging.String("status", string(state.Status)),
			logging.Error(err))
		return gerrors.NewCodeWithCause(gerrors.Database, "failed to update saga state", err).WithContext("saga_id", state.SagaID)
	}
	return nil
}
