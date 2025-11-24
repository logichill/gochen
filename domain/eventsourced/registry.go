package eventsourced

import (
	"context"
	"fmt"

	"gochen/eventing/bus"
	"gochen/eventing/projection"
)

// EventSourcedAutoRegistrar 简化事件处理器与投影注册
type EventSourcedAutoRegistrar struct {
	eventBus          bus.IEventBus
	projectionManager *projection.ProjectionManager
}

// NewEventSourcedAutoRegistrar 创建注册器
func NewEventSourcedAutoRegistrar(eventBus bus.IEventBus, manager *projection.ProjectionManager) *EventSourcedAutoRegistrar {
	return &EventSourcedAutoRegistrar{
		eventBus:          eventBus,
		projectionManager: manager,
	}
}

// RegisterHandlers 注册事件处理器
func (r *EventSourcedAutoRegistrar) RegisterHandlers(ctx context.Context, handlers ...bus.IEventHandler) error {
	if r.eventBus == nil {
		return fmt.Errorf("event bus is nil")
	}
	for _, handler := range handlers {
		if handler == nil {
			continue
		}
		if err := r.eventBus.SubscribeHandler(ctx, handler); err != nil {
			return fmt.Errorf("subscribe handler %s failed: %w", handler.GetHandlerName(), err)
		}
	}
	return nil
}

// UnregisterHandlers 取消注册事件处理器
func (r *EventSourcedAutoRegistrar) UnregisterHandlers(ctx context.Context, handlers ...bus.IEventHandler) error {
	if r.eventBus == nil {
		return fmt.Errorf("event bus is nil")
	}
	for _, handler := range handlers {
		if handler == nil {
			continue
		}
		if err := r.eventBus.UnsubscribeHandler(ctx, handler); err != nil {
			return fmt.Errorf("unsubscribe handler %s failed: %w", handler.GetHandlerName(), err)
		}
	}
	return nil
}

// RegisterProjections 注册投影
func (r *EventSourcedAutoRegistrar) RegisterProjections(projections ...projection.IProjection) error {
	if r.projectionManager == nil {
		return fmt.Errorf("projection manager is nil")
	}
	for _, proj := range projections {
		if proj == nil {
			continue
		}
		if err := r.projectionManager.RegisterProjection(proj); err != nil {
			return fmt.Errorf("register projection %s failed: %w", proj.GetName(), err)
		}
	}
	return nil
}

// UnregisterProjections 取消注册投影
func (r *EventSourcedAutoRegistrar) UnregisterProjections(names ...string) error {
	if r.projectionManager == nil {
		return fmt.Errorf("projection manager is nil")
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if err := r.projectionManager.UnregisterProjection(name); err != nil {
			return fmt.Errorf("unregister projection %s failed: %w", name, err)
		}
	}
	return nil
}
