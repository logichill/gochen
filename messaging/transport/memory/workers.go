// Package memory 实现 Worker 池管理。
package memory

import (
	"context"
	"sync"

	"gochen/contextx"
	"gochen/errors"
	"gochen/messaging"
)

// Start 初始化队列和 worker 池，使内存传输进入可消费状态。
func (t *MemoryTransport) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.mutex.Lock()
	if t.running {
		t.mutex.Unlock()
		return errors.NewCode(errors.Conflict, "memory transport is already running")
	}
	if t.closing {
		t.mutex.Unlock()
		return errors.NewCode(errors.Conflict, "memory transport is closing")
	}

	// 允许 Close 后重启：每次 Start 都重建队列与 worker stop channel。
	t.queue = make(chan messaging.IMessage, t.queueSize)
	t.workers = make([]chan struct{}, t.workerCount)
	t.processing = make(map[int]messaging.IMessage, t.workerCount)
	t.forceStop = false
	t.stoppedPending = nil
	t.workerClaims = make([]*sync.Mutex, t.workerCount)

	t.running = true

	// Worker 使用独立的内部 ctx，避免 Start(ctx) 的取消导致 worker 意外退出。
	//
	// 约定：
	// - worker ctx 不承载业务链路语义（trace/tenant/operator），这些信息应从 message.Metadata 派生；
	// - StopWithSnapshot 超时/取消时，会尽力 cancel worker ctx，使阻塞在下游调用的 handler 能更快退出（若其尊重 ctx）。
	workerCtx := contextx.Background()
	workerCtx, _ = contextx.WithTraceID(workerCtx, "")
	workerCtx, cancel := context.WithCancel(workerCtx)
	t.workerCancel = cancel

	// 启动工作协程
	for i := 0; i < t.workerCount; i++ {
		stopCh := make(chan struct{})
		t.workers[i] = stopCh
		claimGate := &sync.Mutex{}
		t.workerClaims[i] = claimGate

		t.wg.Add(1)
		go t.worker(workerCtx, t.queue, i, stopCh, claimGate)
	}

	t.mutex.Unlock()
	return nil
}

// Stop 关闭传输层并等待 worker 收尾。
func (t *MemoryTransport) Stop(ctx context.Context) error {
	_, err := t.StopWithSnapshot(ctx)
	return err
}

// StopWithSnapshot 停止传输层，并返回仍未确认完成分发的消息快照。
//
// 说明：
// - 正常停止会等待 worker drain 完成，返回值通常为空；
// - ctx 超时/取消时返回“正在分发 + 队列缓冲区尚未取走”的 at-least-once 快照；
// - 快照可能包含已被 handler 接收但尚未返回的消息，调用方重投时需按 message ID 做幂等。
func (t *MemoryTransport) StopWithSnapshot(ctx context.Context) ([]messaging.IMessage, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	t.mutex.Lock()
	if !t.running {
		t.mutex.Unlock()
		return nil, messaging.NewTransportAlreadyStoppedError("memory transport is not running")
	}

	// 标记为已停止，并复制 queue 引用，避免在持锁状态下阻塞等待
	t.running = false
	t.closing = true
	queue := t.queue
	workers := append([]chan struct{}(nil), t.workers...)
	workerClaims := append([]*sync.Mutex(nil), t.workerClaims...)
	cancel := t.workerCancel
	t.mutex.Unlock()

	// 关闭队列，Worker 将在读取完缓冲中的消息后自然退出
	close(queue)

	// 不主动关闭 stopCh，避免抢占队列 flush；队列关闭后 worker 会自然退出

	// 等待所有工作协程结束
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()

	var pending []messaging.IMessage

	select {
	case <-done:
		if cancel != nil {
			cancel()
		}
		// 读取剩余未消费的消息：
		// - workerCount=0（测试）时，这里会把队列中所有消息都返回给调用方；
		// - 正常情况下 worker 会在队列关闭后 drain 完成，pending 通常为空。
		for msg := range queue {
			pending = append(pending, msg)
		}
		t.mutex.Lock()
		t.queue = nil
		t.workers = nil
		t.processing = nil
		t.forceStop = false
		t.stoppedPending = nil
		t.workerClaims = nil
		t.closing = false
		t.workerCancel = nil
		t.mutex.Unlock()

		return pending, nil
	case <-ctx.Done():
		if cancel != nil {
			cancel()
		}
		// 超时返回：停止继续消费，并返回“正在处理 + 仍在队列”的 at-least-once 快照。
		t.mutex.Lock()
		t.forceStop = true
		t.mutex.Unlock()
		for _, ch := range workers {
			if ch == nil {
				continue
			}
			func() {
				defer func() { _ = recover() }()
				close(ch)
			}()
		}
		waitWorkerClaims(workerClaims)

		t.mutex.RLock()
		for _, msg := range t.processing {
			pending = append(pending, msg)
		}
		pending = append(pending, t.stoppedPending...)
		t.mutex.RUnlock()

		for {
			msg, ok := <-queue
			if !ok {
				break
			}
			pending = append(pending, msg)
		}

		// 背景回收：等待 worker 全退出后再允许 Start。
		go func() {
			t.wg.Wait()
			t.mutex.Lock()
			t.queue = nil
			t.workers = nil
			t.processing = nil
			t.forceStop = false
			t.stoppedPending = nil
			t.workerClaims = nil
			t.closing = false
			t.workerCancel = nil
			t.mutex.Unlock()
		}()

		return pending, ctx.Err()
	}

}

// worker 持续从队列取消息并分发给已注册的处理器。
func (t *MemoryTransport) worker(ctx context.Context, queue <-chan messaging.IMessage, workerID int, stopCh chan struct{}, claimGate *sync.Mutex) {
	defer t.wg.Done()

	for {
		message, ok := t.claimMessage(queue, workerID, stopCh, claimGate)
		if !ok {
			return
		}
		func() {
			defer t.clearProcessing(workerID)
			t.dispatch(ctx, message)
		}()
	}
}

func (t *MemoryTransport) claimMessage(queue <-chan messaging.IMessage, workerID int, stopCh chan struct{}, claimGate *sync.Mutex) (messaging.IMessage, bool) {
	claimGate.Lock()
	unlockClaim := true
	defer func() {
		if unlockClaim {
			claimGate.Unlock()
		}
	}()

	if t.shouldStopClaiming() {
		return nil, false
	}

	select {
	case message, ok := <-queue:
		if !ok {
			return nil, false
		}
		if !t.setProcessing(workerID, message) {
			return nil, false
		}
		claimGate.Unlock()
		unlockClaim = false
		return message, true
	case <-stopCh:
		return nil, false
	}
}

func (t *MemoryTransport) shouldStopClaiming() bool {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.forceStop
}

func waitWorkerClaims(claims []*sync.Mutex) {
	for _, claim := range claims {
		if claim == nil {
			continue
		}
		claim.Lock()
		claim.Unlock()
	}
}

func (t *MemoryTransport) setProcessing(workerID int, message messaging.IMessage) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.forceStop {
		t.stoppedPending = append(t.stoppedPending, message)
		return false
	}
	if t.processing != nil {
		t.processing[workerID] = message
	}
	return true
}

func (t *MemoryTransport) clearProcessing(workerID int) {
	t.mutex.Lock()
	if t.processing != nil {
		delete(t.processing, workerID)
	}
	t.mutex.Unlock()
}
