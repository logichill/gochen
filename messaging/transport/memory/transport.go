// Package memory 提供基于内存队列的消息传输实现。
// 适用于单机部署、开发环境和测试场景。
package memory

import (
	"context"
	"strings"
	"sync"

	"gochen/errors"
	"gochen/logging"
	"gochen/messaging"
	"gochen/messaging/deadletter"
)

const (
	defaultQueueSize   = 1000
	defaultWorkerCount = 4
)

// MemoryTransport 使用内存队列和 worker 池实现异步消息传输。
type MemoryTransport struct {
	handlers    map[string][]messaging.IMessageHandler
	queue       chan messaging.IMessage
	queueSize   int
	workerCount int
	workers     []chan struct{}
	running     bool
	closing     bool
	mutex       sync.RWMutex
	wg          sync.WaitGroup
	logger      logging.ILogger

	// deadLetterSink 用于记录 handler 处理失败的消息（可选）。
	deadLetterSink deadletter.ISink

	// workerCancel 用于在 StopWithSnapshot 超时/取消时，尽力取消正在执行的 handler（若 handler 尊重 ctx）。
	workerCancel context.CancelFunc
}

// NewMemoryTransport 创建一个用于运行环境的内存传输实现。
func NewMemoryTransport(queueSize, workerCount int) *MemoryTransport {
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	if workerCount <= 0 {
		workerCount = defaultWorkerCount
	}

	return newMemoryTransport(queueSize, workerCount)
}

// NewMemoryTransportForTest 创建一个默认不启动 worker 的测试用内存传输。
func NewMemoryTransportForTest(queueSize int) *MemoryTransport {
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	return newMemoryTransport(queueSize, 0)
}

// newMemoryTransport 复用初始化逻辑构造内存传输实例。
func newMemoryTransport(queueSize, workerCount int) *MemoryTransport {
	return &MemoryTransport{
		handlers:    make(map[string][]messaging.IMessageHandler),
		queue:       make(chan messaging.IMessage, queueSize),
		queueSize:   queueSize,
		workerCount: workerCount,
		workers:     make([]chan struct{}, workerCount),
		logger:      logging.ComponentLogger("messaging.transport.memory"),
	}
}

// Publish 把一条消息投递到内存队列，等待 worker 异步处理。
func (t *MemoryTransport) Publish(ctx context.Context, message messaging.IMessage) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if message == nil {
		return errors.NewCode(errors.InvalidInput, "message is nil")
	}
	if strings.TrimSpace(message.GetType()) == "" {
		return errors.NewCode(errors.InvalidInput, "message type is required")
	}

	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if !t.running {
		return errors.NewCode(errors.Conflict, "memory transport is not running")
	}

	select {
	case t.queue <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.NewCodeWithCause(errors.Queue, "message queue is full", nil)
	}
}

// PublishAll 按顺序把一批消息写入内存队列。
func (t *MemoryTransport) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if len(messages) == 0 {
		return nil
	}

	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if !t.running {
		return errors.NewCode(errors.Conflict, "memory transport is not running")
	}

	for _, message := range messages {
		if message == nil {
			return errors.NewCode(errors.InvalidInput, "message is nil")
		}
		if strings.TrimSpace(message.GetType()) == "" {
			return errors.NewCode(errors.InvalidInput, "message type is required")
		}

		select {
		case t.queue <- message:
		case <-ctx.Done():
			return ctx.Err()
		default:
			return errors.NewCodeWithCause(errors.Queue, "message queue is full", nil)
		}
	}

	return nil
}

// Stats 返回当前队列深度、worker 数和订阅概览。
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

// IsSynchronous 返回 false，表明该传输是异步的。
func (t *MemoryTransport) IsSynchronous() bool { return false }

// SetDeadLetterSink 配置处理失败消息的死信记录器。
func (t *MemoryTransport) SetDeadLetterSink(sink deadletter.ISink) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.deadLetterSink = sink
}
