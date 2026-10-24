package memory

import (
	"context"
	"strings"

	"gochen/errors"
	"gochen/messaging"
)

// Publish 把一条消息投递到内存队列，等待 worker 异步处理。
func (t *MemoryTransport) Publish(ctx context.Context, message messaging.IMessage) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if messaging.IsNilMessage(message) {
		return errors.NewCode(errors.InvalidInput, "message is nil")
	}
	if strings.TrimSpace(message.GetType()) == "" {
		return errors.NewCode(errors.InvalidInput, "message type is required")
	}

	// 克隆会执行用户代码且开销较大，连拿到 publishMu 之前就应先在无锁路径
	// 上确认 transport 处于运行态：既避免对已停止的 transport 做无谓深拷贝，
	// 也避免下掉粗粒度锁。
	t.mutex.RLock()
	running := t.running
	t.mutex.RUnlock()
	if !running {
		return errors.NewCode(errors.Conflict, "memory transport is not running")
	}

	// 反射深拷贝会执行用户代码且开销较大。它不需要 publishMu 保护
	// （仅读入参 message），因此先克隆再取锁，避免所有并发发布协程
	// 在克隆阶段被序列化。
	queuedMessage, cloneErr := cloneMessageEnvelope(message)
	if cloneErr != nil {
		return cloneErr
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
		if messaging.IsNilMessage(message) {
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
