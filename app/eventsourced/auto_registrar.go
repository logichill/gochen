package eventsourced

import (
	"context"

	"gochen/errors"
	"gochen/eventing/bus"
	"gochen/eventing/projection"
	"gochen/messaging"
)

// EventSourcedAutoRegistrar 简化事件处理器与投影注册。
type EventSourcedAutoRegistrar struct {
	eventBus            bus.IEventBus
	projectionRegistrar projection.IProjectionRegistrar
}

// NewEventSourcedAutoRegistrar 创建事件SourcedAutoRegistrar。
func NewEventSourcedAutoRegistrar(eventBus bus.IEventBus, registrar projection.IProjectionRegistrar) *EventSourcedAutoRegistrar {
	return &EventSourcedAutoRegistrar{
		eventBus:            eventBus,
		projectionRegistrar: registrar,
	}
}

// RegisterHandlers 注册事件。
//
// 说明：
// - RegisterHandlers 注册事件处理器。
func (r *EventSourcedAutoRegistrar) RegisterHandlers(ctx context.Context, handlers ...bus.IEventHandler) ([]messaging.UnsubscribeFunc, error) {
	if r.eventBus == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event bus is nil")
	}
	unsubs := make([]messaging.UnsubscribeFunc, 0, len(handlers))
	for _, handler := range handlers {
		if handler == nil {
			continue
		}
		unsub, err := r.eventBus.SubscribeHandler(ctx, handler)
		if err != nil {
			// 回滚已订阅的处理器，避免半注册状态。
			for i := len(unsubs) - 1; i >= 0; i-- {
				_ = unsubs[i](ctx)
			}
			return nil, errors.Wrap(err, errors.Dependency, "subscribe handler failed").
				WithContext("handler", handler.HandlerName())
		}
		unsubs = append(unsubs, unsub)
	}
	return unsubs, nil
}

func (r *EventSourcedAutoRegistrar) UnregisterHandlers(ctx context.Context, unsubs ...messaging.UnsubscribeFunc) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	for i := len(unsubs) - 1; i >= 0; i-- {
		if unsubs[i] == nil {
			continue
		}
		if err := unsubs[i](ctx); err != nil {
			return errors.Wrap(err, errors.Dependency, "unsubscribe handler failed").
				WithContext("index", i)
		}
	}
	return nil
}

// RegisterProjections 注册Projections。
func (r *EventSourcedAutoRegistrar) RegisterProjections(ctx context.Context, projections ...any) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if r.projectionRegistrar == nil {
		return errors.NewCode(errors.InvalidInput, "projection registrar is nil")
	}
	for _, proj := range projections {
		if proj == nil {
			continue
		}
		if err := r.projectionRegistrar.RegisterProjectionAny(ctx, proj); err != nil {
			return errors.Wrap(err, errors.Dependency, "register projection failed").
				WithContext("projection", projectionName(proj))
		}
	}
	return nil
}

// UnregisterProjections 取消注册投影。
func (r *EventSourcedAutoRegistrar) UnregisterProjections(names ...string) error {
	manager, ok := r.projectionRegistrar.(interface {
		UnregisterProjection(string) error
	})
	if !ok || manager == nil {
		return errors.NewCode(errors.InvalidInput, "projection unregister is not supported")
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if err := manager.UnregisterProjection(name); err != nil {
			return errors.Wrap(err, errors.Dependency, "unregister projection failed").
				WithContext("projection", name)
		}
	}
	return nil
}

func projectionName(proj any) string {
	named, ok := proj.(interface {
		Name() string
	})
	if !ok || named == nil {
		return ""
	}
	return named.Name()
}
