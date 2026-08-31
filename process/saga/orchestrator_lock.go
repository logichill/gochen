package saga

import (
	"context"
	"sync"

	"gochen/errors"
	"gochen/process/lock"
	"gochen/process/task"
)

func (o *SagaOrchestrator) acquireSagaLock(ctx context.Context, sagaID string) (context.Context, func(), error) {
	if o.lock == nil {
		return ctx, func() {}, nil
	}
	if leaseProvider, ok := o.lock.(lock.ILeaseLockProvider); ok {
		lease, err := leaseProvider.AcquireLease(ctx, sagaID)
		if err != nil {
			return nil, nil, wrapSagaLockAcquireError(err, sagaID)
		}
		leaseCtx, stopWatching := watchSagaLeaseLost(ctx, o.supervisor, lease)
		return leaseCtx, func() {
			stopWatching()
			lease.Release()
		}, nil
	}

	release, err := o.lock.Acquire(ctx, sagaID)
	if err != nil {
		return nil, nil, wrapSagaLockAcquireError(err, sagaID)
	}
	return ctx, release, nil
}

func watchSagaLeaseLost(ctx context.Context, supervisor *task.TaskSupervisor, lease lock.ILockLease) (context.Context, func()) {
	leaseCtx, cancel := context.WithCancelCause(ctx)
	released := make(chan struct{})
	done := make(chan struct{})
	if supervisor == nil {
		close(done)
		cancel(errors.NewCode(errors.FailedPrecondition, "saga task supervisor is nil"))
		return leaseCtx, func() { cancel(nil) }
	}
	if err := supervisor.Go(leaseCtx, "lease_lost", func(context.Context) {
		defer close(done)
		select {
		case lostErr, ok := <-lease.Lost():
			if ok && lostErr != nil {
				cancel(lostErr)
			}
		case <-released:
		}
	}); err != nil {
		close(done)
		cancel(err)
	}

	var once sync.Once
	return leaseCtx, func() {
		once.Do(func() {
			close(released)
			<-done
			cancel(nil)
		})
	}
}

func wrapSagaLockAcquireError(err error, sagaID string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	code := errors.Code(err)
	if errors.Is(err, context.DeadlineExceeded) {
		code = errors.Timeout
	}
	return errors.NewCodeWithCause(code, "failed to acquire saga lock", err).
		WithContext("saga_id", sagaID)
}

func ensureSagaLeaseActive(ctx context.Context, sagaID string) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil").WithContext("saga_id", sagaID)
	}
	if err := context.Cause(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return errors.NewCodeWithCause(errors.Conflict, "saga lock lease lost", err).
			WithContext("saga_id", sagaID)
	}
	return nil
}
