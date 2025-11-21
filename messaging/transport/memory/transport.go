// Package memory 提供基于内存队列的消息传输实现
// 适用于单机部署、开发环境和测试场景
package memory

import (
	"context"
	"fmt"
	"sync"

	"gochen/messaging"
)

// MemoryTransport 内存消息传输实现
//
// 特性:
//   - 基于内存队列的异步消息传输
//   - Worker 池模式处理消息
//   - 支持批量发布
//   - 并发安全
//   - 统计信息收集
//
// 使用场景:
//   - 单机部署
//   - 开发环境
//   - 测试场景
//   - 低延迟要求
type MemoryTransport struct {
	handlers    map[string][]messaging.IMessageHandler
	queue       chan messaging.IMessage
	queueSize   int
	workerCount int
	workers     []chan struct{}
	running     bool
	mutex       sync.RWMutex
	wg          sync.WaitGroup
}

// NewMemoryTransport 创建内存传输实例
//
// 参数:
//   - queueSize: 队列大小（<=0 时使用默认 1000）
//   - workerCount: Worker 数量（<=0 时使用默认 4）
//
// 返回:
//   - *MemoryTransport: 传输实例
func NewMemoryTransport(queueSize, workerCount int) *MemoryTransport {
	if queueSize <= 0 {
		queueSize = 1000
	}
	if workerCount <= 0 {
		workerCount = 4
	}

	return newMemoryTransport(queueSize, workerCount)
}

// NewMemoryTransportForTest 创建仅用于测试的内存传输实例
//
// 特性：
//   - 当需要验证队列 drain 等行为时，允许创建 0 worker 的 Transport；
//   - 生产代码应始终使用 NewMemoryTransport，避免无 worker 导致消息永远不被消费。
func NewMemoryTransportForTest(queueSize int) *MemoryTransport {
	if queueSize <= 0 {
		queueSize = 1000
	}
	return newMemoryTransport(queueSize, 0)
}

// newMemoryTransport 内部构造函数，复用初始化逻辑。
func newMemoryTransport(queueSize, workerCount int) *MemoryTransport {
	return &MemoryTransport{
		handlers:    make(map[string][]messaging.IMessageHandler),
		queue:       make(chan messaging.IMessage, queueSize),
		queueSize:   queueSize,
		workerCount: workerCount,
		workers:     make([]chan struct{}, workerCount),
	}
}

// Publish 发布消息到队列
//
// 消息将被放入队列，由 Worker 池异步处理
//
// 参数:
//   - ctx: 上下文
//   - message: 待发布的消息
//
// 返回:
//   - error: 队列满或传输未启动时返回错误
func (t *MemoryTransport) Publish(ctx context.Context, message messaging.IMessage) error {
	t.mutex.RLock()
	running := t.running
	t.mutex.RUnlock()

	if !running {
		return fmt.Errorf("memory transport is not running")
	}

	select {
	case t.queue <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("message queue is full")
	}
}

// PublishAll 批量发布消息到队列
//
// 批量操作，提高吞吐量
//
// 参数:
//   - ctx: 上下文
//   - messages: 待发布的消息列表
//
// 返回:
//   - error: 任一消息失败则返回错误
func (t *MemoryTransport) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	if len(messages) == 0 {
		return nil
	}

	t.mutex.RLock()
	running := t.running
	t.mutex.RUnlock()

	if !running {
		return fmt.Errorf("memory transport is not running")
	}

	for _, message := range messages {
		select {
		case t.queue <- message:
		case <-ctx.Done():
			return ctx.Err()
		default:
			return fmt.Errorf("message queue is full")
		}
	}

	return nil
}

// Stats 获取统计信息
//
// 返回:
//   - TransportStats: 统计信息（运行状态、队列深度、Worker数等）
func (t *MemoryTransport) Stats() messaging.TransportStats {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	handlerCount := 0
	messageTypes := make([]string, 0, len(t.handlers))

	for messageType, handlers := range t.handlers {
		messageTypes = append(messageTypes, messageType)
		handlerCount += len(handlers)
	}

	return messaging.TransportStats{
		Running:      t.running,
		HandlerCount: handlerCount,
		MessageTypes: messageTypes,
		QueueSize:    t.queueSize,
		QueueDepth:   len(t.queue),
		WorkerCount:  t.workerCount,
	}
}
