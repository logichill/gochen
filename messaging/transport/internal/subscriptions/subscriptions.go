package subscriptions

import (
	"context"
	"strings"
	"sync"

	"gochen/errors"
	"gochen/messaging"
)

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

	mu.Lock()
	if handlers[messageType] == nil {
		handlers[messageType] = make([]messaging.IMessageHandler, 0)
	}
	handlers[messageType] = append(handlers[messageType], handler)
	mu.Unlock()

	var once sync.Once
	return func(unsubCtx context.Context) error {
		if unsubCtx == nil {
			return errors.NewCode(errors.InvalidInput, "ctx is nil")
		}
		var err error
		once.Do(func() {
			err = Unsubscribe(unsubCtx, mu, handlers, messageType, handler)
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
		if h == handler {
			handlers[messageType] = append(current[:i], current[i+1:]...)
			return nil
		}
	}

	return errors.NewCode(errors.NotFound, "handler not found for message type").
		WithContext("message_type", messageType)
}
