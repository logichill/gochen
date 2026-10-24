package direct

import (
	"context"

	"gochen/errors"
	"gochen/messaging"
)

// Start 将传输层切换到可发布状态。
func (t *SyncTransport) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.running {
		return errors.NewCode(errors.Conflict, "sync transport is already running")
	}
	t.running = true
	return nil
}

// Stop 关闭传输层，之后不再接受新的发布请求。
func (t *SyncTransport) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	if !t.running {
		return messaging.NewTransportAlreadyStoppedError("sync transport is not running")
	}
	t.running = false
	return nil
}

// StopWithSnapshot 以统一 shutdown 签名包装 Stop；同步传输不会留下待处理消息。
func (t *SyncTransport) StopWithSnapshot(ctx context.Context) ([]messaging.IMessage, error) {
	if err := t.Stop(ctx); err != nil {
		return nil, err
	}
	return nil, nil
}
