// Package memory 提供基于内存队列的消息传输实现。
// 适用于单机部署、开发环境和测试场景。
package memory

import (
	"context"
	"sync"

	"gochen/messaging"
	"gochen/messaging/deadletter"
	"gochen/observe/logging"
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
	closedCh    chan struct{}
	mutex       sync.RWMutex
	publishMu   sync.Mutex
	wg          sync.WaitGroup
	logger      logging.ILogger

	// deadLetterSink 用于记录 handler 处理失败的消息（可选）。
	deadLetterSink deadletter.ISink

	// processing 记录 worker 已取出但尚未完成分发的消息，用于 StopWithSnapshot 超时时返回 at-least-once 快照。
	processing map[int]messaging.IMessage

	// forceStop 表示 StopWithSnapshot 已超时，worker 不应再开始分发新取出的消息。
	forceStop bool

	// stoppedPending 记录 forceStop 后已被 worker 从队列取出但尚未分发的消息。
	stoppedPending []messaging.IMessage

	// workerClaims 用于同步 worker 从队列取出消息到登记 processing 的短暂窗口。
	workerClaims []*sync.Mutex

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
