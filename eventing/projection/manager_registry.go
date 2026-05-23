package projection

import (
	"context"
	"gochen/contextx"
	gerrors "gochen/errors"
	"gochen/logging"
)

// RegisterProjection 用默认后台上下文注册一个投影。
func (pm *ProjectionManager[ID]) RegisterProjection(projection IProjection[ID]) error {
	return pm.RegisterProjectionWithContext(contextx.Background(), projection)
}

// RegisterProjectionWithContext 注册投影，并为其订阅所需的事件类型。
func (pm *ProjectionManager[ID]) RegisterProjectionWithContext(ctx context.Context, projection IProjection[ID]) error {
	if ctx == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "ctx is nil")
	}
	if pm == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "projection manager is nil")
	}
	if pm.eventBus == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "event bus is nil")
	}
	if projection == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "projection cannot be nil")
	}

	name := projection.Name()
	if name == "" {
		return gerrors.NewCode(gerrors.InvalidInput, "projection name cannot be empty")
	}

	pm.mutex.RLock()
	_, exists := pm.runtimes[name]
	checkpointStore := pm.checkpointStore
	pm.mutex.RUnlock()
	if exists {
		return gerrors.NewCode(gerrors.Conflict, "projection already registered").
			WithContext("projection", name)
	}
	if err := validateCheckpointingProjection(projection, checkpointStore); err != nil {
		return err
	}

	rt := newProjectionRuntime(projection)

	subscribedHandlers := make(map[string]*projectionEventHandler[ID])
	for _, eventType := range projection.SupportedEventTypes() {
		handler := &projectionEventHandler[ID]{runtime: rt, manager: pm}
		rt.handlers[eventType] = handler

		unsub, err := pm.eventBus.SubscribeEvent(ctx, eventType, handler)
		if err != nil {
			// rollback registration state to avoid partial registration
			rt.deactivate()
			for t, h := range subscribedHandlers {
				if h != nil && h.unsub != nil {
					if unSubErr := h.unsub(ctx); unSubErr != nil {
						pm.logger.Warn(ctx, "failed to unsubscribe during rollback", logging.Error(unSubErr),
							logging.String("projection", name), logging.String("event_type", t))
					}
				}
			}
			return gerrors.Wrap(err, gerrors.Dependency, "failed to subscribe to event type").
				WithContext("projection", name).
				WithContext("event_type", eventType)
		}
		handler.unsub = unsub
		subscribedHandlers[eventType] = handler
	}

	pm.mutex.Lock()
	var registerErr error
	if _, exists := pm.runtimes[name]; exists {
		registerErr = gerrors.NewCode(gerrors.Conflict, "projection already registered").
			WithContext("projection", name)
	} else if err := validateCheckpointingProjection(projection, pm.checkpointStore); err != nil {
		registerErr = err
	} else {
		pm.runtimes[name] = rt
	}
	pm.mutex.Unlock()
	if registerErr != nil {
		rt.deactivate()
		for t, h := range subscribedHandlers {
			if h != nil && h.unsub != nil {
				if unSubErr := h.unsub(ctx); unSubErr != nil {
					pm.logger.Warn(ctx, "failed to unsubscribe during rollback", logging.Error(unSubErr),
						logging.String("projection", name), logging.String("event_type", t))
				}
			}
		}
		return registerErr
	}

	pm.logger.Info(ctx, "projection registered", logging.String("projection", name))
	return nil
}

// UnregisterProjection 用默认后台上下文取消注册一个投影。
func (pm *ProjectionManager[ID]) UnregisterProjection(name string) error {
	return pm.UnregisterProjectionWithContext(contextx.Background(), name)
}

// UnregisterProjectionWithContext 解除投影订阅并移除其管理状态。
func (pm *ProjectionManager[ID]) UnregisterProjectionWithContext(ctx context.Context, name string) error {
	if ctx == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "ctx is nil")
	}

	pm.mutex.Lock()
	rt, exists := pm.runtimes[name]
	if !exists {
		pm.mutex.Unlock()
		return gerrors.NewCode(gerrors.NotFound, "projection not found").
			WithContext("projection", name)
	}

	handlers := make(map[string]*projectionEventHandler[ID], len(rt.handlers))
	for eventType, handler := range rt.handlers {
		handlers[eventType] = handler
		delete(rt.handlers, eventType)
	}
	delete(pm.runtimes, name)
	pm.mutex.Unlock()

	rt.deactivate()
	rt.execMu.Lock()
	rt.execMu.Unlock()

	for eventType, handler := range handlers {
		if handler == nil {
			pm.logger.Warn(ctx, "registered handler instance not found, unsubscribe may fail",
				logging.String("projection", name),
				logging.String("event_type", eventType),
			)
		}

		if handler != nil && handler.unsub != nil {
			if err := handler.unsub(ctx); err != nil {
				pm.logger.Warn(ctx, "failed to unsubscribe from event", logging.Error(err),
					logging.String("event_type", eventType),
					logging.String("projection", name),
				)
			}
		}
	}

	pm.logger.Info(ctx, "projection unregistered", logging.String("projection", name))
	return nil
}

// StartProjection 把指定投影状态切换为 running。
func (pm *ProjectionManager[ID]) StartProjection(name string) error {
	rt, exists := pm.runtime(name)
	if !exists {
		return gerrors.NewCode(gerrors.NotFound, "projection not found").
			WithContext("projection", name)
	}

	rt.execMu.Lock()
	defer rt.execMu.Unlock()

	if rt.isRunning() {
		return nil
	}

	rt.markRunning()
	pm.logger.Info(contextx.Background(), "projection started", logging.String("projection", name))
	return nil
}

// StopProjection 把指定投影状态切换为 stopped。
func (pm *ProjectionManager[ID]) StopProjection(name string) error {
	rt, exists := pm.runtime(name)
	if !exists {
		return gerrors.NewCode(gerrors.NotFound, "projection not found").
			WithContext("projection", name)
	}

	rt.execMu.Lock()
	defer rt.execMu.Unlock()

	status := rt.statusCopy()
	if status != nil && status.Status == "stopped" {
		return nil
	}

	rt.markStopped()
	pm.logger.Info(contextx.Background(), "projection stopped", logging.String("projection", name))
	return nil
}

func (pm *ProjectionManager[ID]) ProjectionStatus(name string) (*ProjectionStatus, error) {
	pm.mutex.RLock()
	rt, exists := pm.runtimes[name]
	pm.mutex.RUnlock()
	if !exists {
		return nil, gerrors.NewCode(gerrors.NotFound, "projection not found").
			WithContext("projection", name)
	}

	return rt.statusCopy(), nil
}

func (pm *ProjectionManager[ID]) ProjectionStatuses() map[string]*ProjectionStatus {
	pm.mutex.RLock()
	runtimes := make(map[string]*projectionRuntime[ID], len(pm.runtimes))
	for name, rt := range pm.runtimes {
		runtimes[name] = rt
	}
	pm.mutex.RUnlock()

	result := make(map[string]*ProjectionStatus, len(runtimes))
	for name, rt := range runtimes {
		result[name] = rt.statusCopy()
	}

	return result
}
