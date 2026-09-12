// Package memory 提供基于内存队列的消息传输实现。
// 适用于单机部署、开发环境和测试场景。
package memory

import (
	"context"
	"strings"
	"sync"

	"gochen/errors"
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

	t.publishMu.Lock()
	defer t.publishMu.Unlock()

	t.mutex.RLock()
	if !t.running {
		t.mutex.RUnlock()
		return errors.NewCode(errors.Conflict, "memory transport is not running")
	}
	queue := t.queue
	t.mutex.RUnlock()

	queuedMessage, cloneErr := cloneMessageEnvelope(message)
	if cloneErr != nil {
		return cloneErr
	}

	// 克隆可能执行用户代码；入队时重新确认队列仍属于本轮运行，
	// 并持有状态锁，保证 Stop 不能在发送期间关闭队列。
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	if !t.running || t.queue != queue {
		return errors.NewCode(errors.Conflict, "memory transport stopped while cloning message")
	}
	return publishOne(ctx, queue, queuedMessage)
}

// PublishAll 按顺序把一批消息写入内存队列。
//
// 说明：消息克隆或校验过程中的 panic 会传播给调用方。
// 批量入队会在入队前检查 ctx，开始写入队列后不再逐条监听 ctx.Done。
func (t *MemoryTransport) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if len(messages) == 0 {
		return nil
	}

	for _, message := range messages {
		if message == nil {
			return errors.NewCode(errors.InvalidInput, "message is nil")
		}
		if strings.TrimSpace(message.GetType()) == "" {
			return errors.NewCode(errors.InvalidInput, "message type is required")
		}
	}

	t.publishMu.Lock()
	defer t.publishMu.Unlock()

	t.mutex.RLock()
	if !t.running {
		t.mutex.RUnlock()
		return errors.NewCode(errors.Conflict, "memory transport is not running")
	}
	queue := t.queue
	if cap(queue)-len(queue) < len(messages) {
		t.mutex.RUnlock()
		return errors.NewCodeWithCause(errors.Queue, "message queue is full", nil)
	}
	t.mutex.RUnlock()

	cloned := make([]messaging.IMessage, len(messages))
	for i, message := range messages {
		clonedMessage, cloneErr := cloneMessageEnvelope(message)
		if cloneErr != nil {
			return cloneErr.WithContext("index", i)
		}
		cloned[i] = clonedMessage
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.mutex.RLock()
	if !t.running || t.queue != queue {
		t.mutex.RUnlock()
		return errors.NewCode(errors.Conflict, "memory transport stopped while cloning messages")
	}
	if cap(queue)-len(queue) < len(cloned) {
		t.mutex.RUnlock()
		return errors.NewCodeWithCause(errors.Queue, "message queue is full", nil)
	}
	defer t.mutex.RUnlock()

	return publishMany(queue, cloned)
}

func publishOne(ctx context.Context, queue chan messaging.IMessage, message messaging.IMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case queue <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.NewCodeWithCause(errors.Queue, "message queue is full", nil)
	}
}

func publishMany(queue chan messaging.IMessage, messages []messaging.IMessage) error {
	for _, message := range messages {
		queue <- message
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

func cloneMessageEnvelope(message messaging.IMessage) (messaging.IMessage, errors.IAppError) {
	cloned, err := messaging.CloneMessageEnvelope(message)
	if err == nil {
		return cloned, nil
	}
	var appErr *errors.AppError
	if errors.As(err, &appErr) && appErr != nil {
		return nil, appErr
	}
	return nil, errors.Wrap(err, errors.InvalidInput, "clone message envelope failed")
}
