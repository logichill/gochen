package direct

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"

	"gochen/contextx"
	"gochen/errors"
	"gochen/messaging"
)

// Publish 在当前 goroutine 内同步调用所有匹配的消息处理器。
func (t *SyncTransport) Publish(ctx context.Context, message messaging.IMessage) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if messaging.IsNilMessage(message) {
		return errors.NewCode(errors.InvalidInput, "message is nil")
	}
	if strings.TrimSpace(message.GetType()) == "" {
		return errors.NewCode(errors.InvalidInput, "message type is required")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.mutex.RLock()
	if !t.running {
		t.mutex.RUnlock()
		return errors.NewCode(errors.Conflict, "sync transport is not running")
	}

	messageType := message.GetType()
	exact := t.handlers[messageType]
	wildcard := t.handlers["*"]

	// 复制 handlers 切片，防止迭代时被修改导致竞态。
	combinedLen := len(exact) + len(wildcard)
	handlers := make([]messaging.IMessageHandler, 0, combinedLen)
	if len(exact) > 0 {
		handlers = append(handlers, exact...)
	}
	if len(wildcard) > 0 {
		handlers = append(handlers, wildcard...)
	}
	t.mutex.RUnlock()

	if len(handlers) == 0 {
		// 在同步模式下，没有处理器通常不是错误，只是无人监听
		return nil
	}

	// 默认贯通：将 metadata 中的链路信息（tenant/trace/operator）与 message.Metadata 双向补齐；
	// 并确保 trace_id 缺失时能用 message.ID 兜底，便于日志关联。
	derived := ctx
	if md := message.GetMetadata(); md != nil {
		var err error
		derived, err = contextx.DeriveFromMetadata(derived, md)
		if err != nil {
			return err
		}
		fallback := strings.TrimSpace(message.GetID())
		if fallback == "" {
			fallback = contextx.GenerateTraceID()
		}
		derived, err = contextx.EnsureTraceID(derived, md, fallback)
		if err != nil {
			return err
		}
		_ = contextx.InjectTenantID(derived, md)
		_ = contextx.InjectOperator(derived, md)
	}

	var errs []error
	for _, handler := range handlers {
		// 多 Handler 广播共享同一 message 实例时，前一个 Handler 若原地修改
		// payload/metadata 会污染后续 Handler。因此在多 Handler 场景下为每个 Handler 独立克隆副本。
		delivery := message
		if len(handlers) > 1 {
			cloned, cloneErr := messaging.CloneMessageEnvelope(message)
			if cloneErr != nil {
				errs = append(errs, errors.Wrap(cloneErr, errors.Internal, "clone message for handler failed").
					WithContext("handler_type", handler.Type()).
					WithContext("message_id", message.GetID()))
				continue
			}
			delivery = cloned
		}
		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = errors.NewCode(errors.Internal, "message handler panicked").
						WithContext("panic", fmt.Sprint(r)).
						WithContext("stack", string(debug.Stack())).
						WithContext("handler_type", handler.Type()).
						WithContext("message_type", messageType).
						WithContext("message_id", message.GetID())
				}
			}()
			return handler.Handle(derived, delivery)
		}()
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		if len(errs) == 1 {
			return errs[0]
		}
		joined := errors.Join(errs...)
		return errors.Wrap(joined, errors.Internal, "message handling completed with multiple errors").
			WithContext("error_count", len(errs))
	}
	return nil
}

// PublishAll 按顺序同步发布一批消息，遇错立即停止。
func (t *SyncTransport) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	for i, message := range messages {
		if messaging.IsNilMessage(message) {
			return errors.NewCode(errors.InvalidInput, "message is nil").WithContext("index", i)
		}
		if err := t.Publish(ctx, message); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			if appErr, ok := err.(*errors.AppError); ok && appErr != nil {
				return appErr.WithContext("index", i).WithContext("message_id", message.GetID())
			}
			return errors.Wrap(err, errors.Internal, "failed to publish message").
				WithContext("index", i).
				WithContext("message_id", message.GetID())
		}
	}
	return nil
}
