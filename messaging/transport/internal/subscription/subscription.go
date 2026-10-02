package subscription

import (
	"context"
	"strings"
	"sync"

	"gochen/errors"
	"gochen/messaging"
)

// registration 为每次订阅提供独立、可比较的标识，处理器本身可以是函数或含 slice 的值。
type registration struct {
	messaging.IMessageHandler
}

func Subscribe(
	ctx context.Context,
	mu *sync.RWMutex,
	handlers map[string][]messaging.IMessageHandler,
	messageType string,
	handler messaging.IMessageHandler,
) (messaging.UnsubscribeFunc, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if strings.TrimSpace(messageType) == "" {
		return nil, errors.NewCode(errors.InvalidInput, "messageType is required")
	}
	if handler == nil {
		return nil, errors.NewCode(errors.InvalidInput, "handler is nil")
	}
	if mu == nil {
		return nil, errors.NewCode(errors.InvalidInput, "mutex is nil")
	}
	if handlers == nil {
		return nil, errors.NewCode(errors.InvalidInput, "handlers map is nil")
	}

	registered := &registration{IMessageHandler: handler}
	mu.Lock()
	if handlers[messageType] == nil {
		handlers[messageType] = make([]messaging.IMessageHandler, 0)
	}
	handlers[messageType] = append(handlers[messageType], registered)
	mu.Unlock()

	var once sync.Once
	return func(unsubCtx context.Context) error {
		if unsubCtx == nil {
			return errors.NewCode(errors.InvalidInput, "ctx is nil")
		}
		var err error
		once.Do(func() {
			err = Unsubscribe(unsubCtx, mu, handlers, messageType, registered)
		})
		return err
	}, nil
}

func Unsubscribe(
	ctx context.Context,
	mu *sync.RWMutex,
	handlers map[string][]messaging.IMessageHandler,
	messageType string,
	handler messaging.IMessageHandler,
) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if mu == nil {
		return errors.NewCode(errors.InvalidInput, "mutex is nil")
	}
	if handlers == nil {
		return errors.NewCode(errors.InvalidInput, "handlers map is nil")
	}

	mu.Lock()
	defer mu.Unlock()

	current, ok := handlers[messageType]
	if !ok {
		return errors.NewCode(errors.NotFound, "no handlers for message type").
			WithContext("message_type", messageType)
	}

	for i, h := range current {
		if registered, ok := h.(*registration); ok && registered == handler {
			copy(current[i:], current[i+1:])
			current[len(current)-1] = nil
			if len(current) == 1 {
				delete(handlers, messageType)
			} else {
				handlers[messageType] = current[:len(current)-1]
			}
			return nil
		}
	}

	return errors.NewCode(errors.NotFound, "handler not found for message type").
		WithContext("message_type", messageType)
}
