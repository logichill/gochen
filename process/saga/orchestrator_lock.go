package saga

import (
	"context"
	"sync"

	"gochen/errors"
	"gochen/process/lock"
	"gochen/process/task"
)

// acquireSagaLock 获取 saga 互斥锁。
//
// 返回值：
//   - ctx：随父上下文取消/超时的租约上下文，用于常规执行路径；
//   - compCtx：补偿专用上下文，与父上下文的取消/截止时间解绑（父取消不中止
//     补偿），但保留租约失效信号（租约丢失时以丢失原因为 Cause 取消），
//     使补偿在 detach 后仍能检测租约失效，不会被父取消连带剥离保护。
//   - release：释放锁与监听。
func (o *SagaOrchestrator) acquireSagaLock(ctx context.Context, sagaID string) (context.Context, context.Context, func(), error) {
	if o.lock == nil {
		return ctx, context.WithoutCancel(ctx), func() {}, nil
	}
	if leaseProvider, ok := o.lock.(lock.ILeaseLockProvider); ok {
		lease, err := leaseProvider.AcquireLease(ctx, sagaID)
		if err != nil {
			return nil, nil, nil, wrapSagaLockAcquireError(err, sagaID)
		}
		leaseCtx, compCtx, stopWatching := watchSagaLeaseLost(ctx, o.supervisor, lease)
		return leaseCtx, compCtx, func() {
			stopWatching()
			lease.Release()
		}, nil
	}

	release, err := o.lock.Acquire(ctx, sagaID)
	if err != nil {
		return nil, nil, nil, wrapSagaLockAcquireError(err, sagaID)
	}
	return ctx, context.WithoutCancel(ctx), release, nil
}

// watchSagaLeaseLost 监听租约丢失信号，返回（leaseCtx, compCtx, stop）：
//   - leaseCtx：WithCancelCause(parent)，租约丢失时以丢失原因为 Cause 取消，
//     父取消/超时按标准 context 语义传播；
//   - compCtx：WithCancelCause(WithoutCancel(parent))，不随父取消/超时取消，
//     仅在租约丢失时以丢失原因取消（或释放时以 nil 取消），供补偿路径使用；
//   - stop：停止监听 goroutine 并释放两个上下文。
func watchSagaLeaseLost(ctx context.Context, supervisor *task.TaskSupervisor, lease lock.ILockLease) (context.Context, context.Context, func()) {
	leaseCtx, cancel := context.WithCancelCause(ctx)
	compCtx, cancelComp := context.WithCancelCause(context.WithoutCancel(ctx))
	released := make(chan struct{})
	done := make(chan struct{})
	if supervisor == nil {
		close(done)
		leaseErr := errors.NewCode(errors.FailedPrecondition, "saga task supervisor is nil")
		cancel(leaseErr)
		cancelComp(leaseErr)
		return leaseCtx, compCtx, func() { cancel(nil); cancelComp(nil) }
	}
	if err := supervisor.Go(leaseCtx, "lease_lost", func(context.Context) {
		defer close(done)
		select {
		case lostErr, ok := <-lease.Lost():
			if ok && lostErr != nil {
				cancel(lostErr)
				cancelComp(lostErr)
			}
		case <-released:
		}
	}); err != nil {
		close(done)
		cancel(err)
		cancelComp(err)
	}

	var once sync.Once
	return leaseCtx, compCtx, func() {
		once.Do(func() {
			close(released)
			<-done
			cancel(nil)
			cancelComp(nil)
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

// isSagaLeaseLostError 判断 ensureSagaLeaseActive 返回的错误是否源于租约丢失。
// ensureSagaLeaseActive 将租约丢失包装为 Conflict；父 ctx 的普通取消/超时按
// 原样返回（非 Conflict）。步骤失败后的守卫只有命中租约丢失才短路失败处理，
// 普通取消必须继续进入补偿（由补偿专用上下文完成回滚）。
func isSagaLeaseLostError(err error) bool {
	return err != nil && errors.Code(err) == errors.Conflict
}
