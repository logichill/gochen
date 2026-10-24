package memory

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"gochen/messaging"
	"gochen/observe/logging"
)

// worker 持续从队列取消息并分发给已注册的处理器。
func (t *MemoryTransport) worker(ctx context.Context, queue <-chan messaging.IMessage, workerID int, stopCh chan struct{}, claimGate *sync.Mutex) {
	defer t.wg.Done()

	for {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		message, ok := t.claimMessage(ctx, queue, workerID, stopCh, claimGate)
		if !ok {
			return
		}
		func() {
			// 顶层 panic 隔离：dispatch 内部虽有 per-handler recover，但
			// 进入 handler 之前的阶段（GetType/GetMetadata/克隆 envelope）
			// 一旦 panic 会击穿 worker 协程；随 worker 逐个退出，整个池子
			// 永久瘫痪。这里捕获异常并记录日志，worker 继续消费后续消息。
			// 注意：不要在 recover 日志中访问 message（很可能是 panic 源）。
			defer func() {
				if r := recover(); r != nil {
					if t.logger != nil {
						t.logger.Error(ctx, "memory transport worker panicked",
							logging.Int("worker_id", workerID),
							logging.String("panic", fmt.Sprint(r)),
							logging.String("stack", string(debug.Stack())),
						)
					}
				}
			}()
			defer t.clearProcessing(workerID)
			t.dispatch(ctx, message)
		}()
	}
}

func (t *MemoryTransport) claimMessage(ctx context.Context, queue <-chan messaging.IMessage, workerID int, stopCh chan struct{}, claimGate *sync.Mutex) (messaging.IMessage, bool) {
	claimGate.Lock()
	unlockClaim := true
	defer func() {
		if unlockClaim {
			claimGate.Unlock()
		}
	}()

	if t.shouldStopClaiming() || (ctx != nil && ctx.Err() != nil) {
		return nil, false
	}

	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}

	select {
	case message, ok := <-queue:
		if !ok {
			return nil, false
		}
		if ctx != nil && ctx.Err() != nil {
			t.setStoppedPending(message)
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
	case <-ctxDone:
		return nil, false
	}
}

func (t *MemoryTransport) setStoppedPending(message messaging.IMessage) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.stoppedPending = append(t.stoppedPending, message)
}

func (t *MemoryTransport) shouldStopClaiming() bool {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.forceStop
}

// waitWorkerClaims 逐个等待 worker 的 claim 门闩空闲。
//
// 这里的空临界区是有意为之的屏障模式：Lock 返回即代表 worker 当前不持有
// 门闩（已进入 dispatch 或退出路径），配合 forceStop 停机确保后续快照不含
// 新领消息，并非遗漏临界区逻辑。
func waitWorkerClaims(claims []*sync.Mutex) {
	for _, claim := range claims {
		if claim == nil {
			continue
		}
		claim.Lock()   //nolint:staticcheck // 有意的屏障等待：Lock 返回即门闩空闲（SA2001）
		claim.Unlock() //nolint:staticcheck // 有意的屏障等待：立即释放（SA2001）
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
