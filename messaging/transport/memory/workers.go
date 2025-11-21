// Package memory 实现 Worker 池管理
package memory

import (
	"context"
	"fmt"
	"time"

	"gochen/messaging"
)

// Start 启动传输层
//
// 启动 Worker 池开始处理消息队列
//
// 参数:
//   - ctx: 上下文（用于取消 Worker）
//
// 返回:
//   - error: 已启动或启动失败时返回错误
func (t *MemoryTransport) Start(ctx context.Context) error {
	t.mutex.Lock()
	if t.running {
		t.mutex.Unlock()
		return fmt.Errorf("memory transport is already running")
	}

	t.running = true

	// 启动工作协程
	for i := 0; i < t.workerCount; i++ {
		stopCh := make(chan struct{})
		t.workers[i] = stopCh

		t.wg.Add(1)
		go t.worker(ctx, i, stopCh)
	}

	t.mutex.Unlock()
	return nil
}

// Close 关闭传输层
//
// 停止所有 Worker 并等待处理中的消息完成
//
// 返回:
//   - error: 未启动或关闭失败时返回错误
func (t *MemoryTransport) Close() error {
	_, err := t.CloseWithContext(context.Background())
	return err
}

// CloseWithTimeout 关闭传输层并在指定时间内等待，超时返回 ctx 错误。
func (t *MemoryTransport) CloseWithTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return t.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := t.CloseWithContext(ctx)
	return err
}

// CloseWithContext 允许调用方控制等待 worker 退出的上下文，并在队列 drain 后返回剩余消息快照。
//
// 语义说明：
//   - 返回的切片仅包含“关闭时仍留在队列缓冲区、且未被 worker 取走”的消息；
//   - 这是 best-effort 快照：在 running=false 之前已被 worker 读取并处理的消息不会出现在结果中；
//   - 调用方不应将返回值视为“所有未处理消息的完整列表”，而只能用于诊断/补偿等场景。
func (t *MemoryTransport) CloseWithContext(ctx context.Context) ([]messaging.IMessage, error) {
	t.mutex.Lock()
	if !t.running {
		t.mutex.Unlock()
		return nil, fmt.Errorf("memory transport is not running")
	}

	// 标记为已停止，并复制 queue 引用，避免在持锁状态下阻塞等待
	t.running = false
	queue := t.queue
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
		// 读取剩余未消费的消息（队列已关闭，worker 已退出，无并发读）
		for {
			msg, ok := <-queue
			if !ok {
				break
			}
			pending = append(pending, msg)
		}
		return pending, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}

}

// worker 工作协程
//
// 从队列中取出消息并分发给订阅的处理器
//
// 参数:
//   - ctx: 上下文
//   - workerID: Worker ID
//   - stopCh: 停止信号通道
func (t *MemoryTransport) worker(ctx context.Context, workerID int, stopCh chan struct{}) {
	defer t.wg.Done()

	for {
		select {
		case message, ok := <-t.queue:
			if !ok {
				return
			}

			t.dispatch(ctx, message)

		case <-stopCh:
			return

		case <-ctx.Done():
			return
		}
	}
}
