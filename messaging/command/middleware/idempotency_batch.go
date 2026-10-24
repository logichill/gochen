package middleware

import (
	"context"
	"sync"

	"gochen/errors"
	"gochen/messaging"
)

type publishAllBatchContextKey struct {
	middleware *IdempotencyMiddleware
}

type publishAllBatch struct {
	mu        sync.Mutex
	pending   map[string]struct{}
	callbacks []func(success bool)
	finished  bool
	success   bool
}

// BeginPublish 为即时发布隔离继承的批次状态，避免已投递命令被外层批次回滚。
// 保留 ctx 的其他值、取消与截止时间；ctx 不含当前实例的批次时原样返回。
func (m *IdempotencyMiddleware) BeginPublish(ctx context.Context) context.Context {
	if _, ok := m.publishAllBatchFromContext(ctx); !ok {
		return ctx
	}
	return context.WithValue(ctx, publishAllBatchContextKey{middleware: m}, (*publishAllBatch)(nil))
}

// BeginPublishAllBatch 创建批量发布提交作用域。
//
// MessageBus.PublishAll 在多消息批量路径中会先逐条执行中间件，再执行一次真实
// transport.PublishAll。命令幂等成功标记必须等 transport 批量提交成功后才能记录。
// 批量持有命令锁到投递完成；锁已被占用时返回 Concurrency，调用方可重试整个批次。
func (m *IdempotencyMiddleware) BeginPublishAllBatch(ctx context.Context) (context.Context, func(success bool)) {
	if ctx == nil {
		ctx = context.Background()
	}
	batch := &publishAllBatch{
		pending: make(map[string]struct{}),
	}
	return context.WithValue(ctx, publishAllBatchContextKey{middleware: m}, batch), batch.finish
}

// BeginPublishAllAttempt 为 MessageBus 的同步中间件调用创建幂等状态检查点。
// 结束函数可重复调用；失败时回滚该检查点后的预留和提交回调，成功时保留到整批完成。
// ctx 不属于当前中间件的批次时返回 nil。
func (m *IdempotencyMiddleware) BeginPublishAllAttempt(ctx context.Context) func(success bool) {
	batch, ok := m.publishAllBatchFromContext(ctx)
	if !ok {
		return nil
	}
	batch.mu.Lock()
	checkpoint := len(batch.callbacks)
	batch.mu.Unlock()
	finished := false
	return func(success bool) {
		batch.mu.Lock()
		if finished || batch.finished {
			batch.mu.Unlock()
			return
		}
		finished = true
		if success || checkpoint >= len(batch.callbacks) {
			batch.mu.Unlock()
			return
		}
		callbacks := append([]func(bool){}, batch.callbacks[checkpoint:]...)
		clear(batch.callbacks[checkpoint:])
		batch.callbacks = batch.callbacks[:checkpoint]
		batch.mu.Unlock()
		for i := len(callbacks) - 1; i >= 0; i-- {
			callbacks[i](false)
		}
	}
}

func (m *IdempotencyMiddleware) handleInPublishAllBatch(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc, commandID string, batch *publishAllBatch) error {
	if !batch.reserve(commandID) {
		return nil
	}
	// 锁争用、next 失败或 panic 后撤销预留，允许同一批次作用域重新尝试。
	keepReservation := false
	defer func() {
		if !keepReservation {
			batch.unreserve(commandID)
		}
	}()

	lock := m.acquireLock(commandID)
	// 批次可能以相反顺序获取多个命令锁，不能持有前面的锁再阻塞等待后面的锁。
	if !lock.mu.TryLock() {
		m.releaseLock(commandID, lock)
		return errors.NewCode(errors.Concurrency, "command is already being processed").WithContext("command_id", commandID)
	}

	releaseOnReturn := true
	defer func() {
		if releaseOnReturn {
			lock.mu.Unlock()
			m.releaseLock(commandID, lock)
		}
	}()

	if m.isProcessed(commandID) {
		batch.addCallback(func(success bool) {
			if !success {
				batch.unreserve(commandID)
			}
		})
		keepReservation = true
		return nil
	}

	err := next(ctx, message)
	if err != nil {
		return err
	}

	releaseOnReturn = false
	batch.addCallback(func(success bool) {
		defer func() {
			lock.mu.Unlock()
			m.releaseLock(commandID, lock)
		}()
		if success {
			m.markProcessed(commandID)
		} else {
			batch.unreserve(commandID)
		}
	})
	keepReservation = true
	return nil
}

func (m *IdempotencyMiddleware) publishAllBatchFromContext(ctx context.Context) (*publishAllBatch, bool) {
	if ctx == nil {
		return nil, false
	}
	batch, ok := ctx.Value(publishAllBatchContextKey{middleware: m}).(*publishAllBatch)
	return batch, ok && batch != nil
}

func (b *publishAllBatch) reserve(commandID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.pending[commandID]; ok {
		return false
	}
	b.pending[commandID] = struct{}{}
	return true
}

func (b *publishAllBatch) unreserve(commandID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, commandID)
}

func (b *publishAllBatch) addCallback(callback func(success bool)) {
	b.mu.Lock()
	if b.finished {
		success := b.success
		b.mu.Unlock()
		callback(success)
		return
	}
	b.callbacks = append(b.callbacks, callback)
	b.mu.Unlock()
}

func (b *publishAllBatch) finish(success bool) {
	b.mu.Lock()
	if b.finished {
		b.mu.Unlock()
		return
	}
	b.finished = true
	b.success = success
	callbacks := append([]func(bool){}, b.callbacks...)
	b.callbacks = nil
	b.mu.Unlock()

	for i := len(callbacks) - 1; i >= 0; i-- {
		callbacks[i](success)
	}
}
