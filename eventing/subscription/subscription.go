package subscription

import (
	"sync"

	"gochen/errors"
	"gochen/eventing/store"
	"gochen/observe/logging"
)

// Subscription 定义Subscription。
type Subscription[ID comparable] struct {
	store   store.IEventStreamStore[ID]
	handler HandlerFunc[ID]
	cfg     *Config
	mu      sync.RWMutex
	cursor  string
	logger  logging.ILogger
}

// New 创建Subscription[ID]。
func New[ID comparable](eventStore store.IEventStreamStore[ID], handler HandlerFunc[ID], cfg *Config) (*Subscription[ID], error) {
	if eventStore == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event store cannot be nil")
	}
	if handler == nil {
		return nil, errors.NewCode(errors.InvalidInput, "handler cannot be nil")
	}
	cfg = cfg.normalize()
	if cfg.CursorStore != nil && cfg.Name == "" {
		return nil, errors.NewCode(errors.InvalidInput, "subscription name cannot be empty when cursor store is configured")
	}
	return &Subscription[ID]{
		store:   eventStore,
		handler: handler,
		cfg:     cfg,
		cursor:  cfg.StartCursor,
		logger:  cfg.Logger,
	}, nil
}
