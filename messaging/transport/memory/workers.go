// Package memory 实现 Worker 池管理
package memory

import (
	"context"
	"fmt"
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
	defer t.mutex.Unlock()

	if t.running {
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

	return nil
}

// Close 关闭传输层
//
// 停止所有 Worker 并等待处理中的消息完成
//
// 返回:
//   - error: 未启动或关闭失败时返回错误
func (t *MemoryTransport) Close() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if !t.running {
		return fmt.Errorf("memory transport is not running")
	}

	t.running = false

	// 停止所有工作协程
	for _, stopCh := range t.workers {
		select {
		case <-stopCh:
			// channel已经关闭
		default:
			close(stopCh)
		}
	}

	// 等待所有工作协程结束
	t.wg.Wait()

	// 关闭队列
	close(t.queue)

	return nil
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
