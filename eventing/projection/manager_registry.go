package projection

import (
	"context"
	"gochen/contextx"
	"gochen/errors"
	"gochen/observe/logging"
)

// RegisterProjection 用默认后台上下文注册一个投影。
func (pm *ProjectionManager[ID]) RegisterProjection(projection IProjection[ID]) error {
	return pm.RegisterProjectionWithContext(contextx.Background(), projection)
}

// RegisterProjectionWithContext 注册投影，并为其订阅所需的事件类型。
// 这是供组合根按名称管理的入口，返回错误不保证订阅已全部回滚。
// 共享 manager 的组件应使用 RegisterProjectionAny 返回的释放函数管理本次注册。
func (pm *ProjectionManager[ID]) RegisterProjectionWithContext(ctx context.Context, projection IProjection[ID]) error {
	_, err := pm.registerProjectionWithContext(ctx, projection)
	return err
}

func (pm *ProjectionManager[ID]) registerProjectionWithContext(ctx context.Context, projection IProjection[ID]) (*projectionRuntime[ID], error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if pm == nil {
		return nil, errors.NewCode(errors.InvalidInput, "projection manager is nil")
	}
	if pm.eventBus == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event bus is nil")
	}
	if isNilProjection(projection) {
		return nil, errors.NewCode(errors.InvalidInput, "projection cannot be nil")
	}

	name := projection.Name()
	if name == "" {
		return nil, errors.NewCode(errors.InvalidInput, "projection name cannot be empty")
	}

	pm.mutex.RLock()
	_, exists := pm.runtimes[name]
	_, registering := pm.registering[name]
	checkpointStore := pm.checkpointStore
	pm.mutex.RUnlock()
	if exists || registering {
		return nil, errors.NewCode(errors.Conflict, "projection already registered").
			WithContext("projection", name)
	}
	if err := validateCheckpointingProjection(projection, checkpointStore); err != nil {
		return nil, err
	}

	rt := newProjectionRuntime(projection)
	pm.mutex.Lock()
	if pm.registering == nil {
		pm.registering = make(map[string]*projectionRuntime[ID])
	}
	if _, exists := pm.runtimes[name]; exists {
		pm.mutex.Unlock()
		return nil, errors.NewCode(errors.Conflict, "projection already registered").WithContext("projection", name)
	}
	if _, exists := pm.registering[name]; exists {
		pm.mutex.Unlock()
		return nil, errors.NewCode(errors.Conflict, "projection already registered").WithContext("projection", name)
	}
	pm.registering[name] = rt
	pm.mutex.Unlock()

	subscribedHandlers := make(map[string]*projectionEventHandler[ID])
	for _, eventType := range projection.SupportedEventTypes() {
		if _, subscribed := subscribedHandlers[eventType]; subscribed {
			continue
		}
		handler := &projectionEventHandler[ID]{runtime: rt, manager: pm}
		rt.handlers[eventType] = handler

		unsub, err := pm.eventBus.SubscribeEvent(ctx, eventType, handler)
		if err != nil {
			subscribeErr := errors.Wrap(err, errors.Dependency, "failed to subscribe to event type").
				WithContext("projection", name).
				WithContext("event_type", eventType)
			kept, rollbackErr := pm.rollbackFailedRegistration(ctx, name, rt, subscribedHandlers)
			if kept {
				return rt, errors.Join(subscribeErr, rollbackErr)
			}
			return nil, subscribeErr
		}
		handler.unsub = unsub
		subscribedHandlers[eventType] = handler
	}

	pm.mutex.Lock()
	registerErr := validateCheckpointingProjection(projection, pm.checkpointStore)
	if registerErr == nil {
		delete(pm.registering, name)
		pm.runtimes[name] = rt
	}
	pm.mutex.Unlock()
	if registerErr != nil {
		kept, rollbackErr := pm.rollbackFailedRegistration(ctx, name, rt, subscribedHandlers)
		if kept {
			return rt, errors.Join(registerErr, rollbackErr)
		}
		return nil, registerErr
	}

	pm.logger.Info(ctx, "projection registered", logging.String("projection", name))
	return rt, nil
}

// rollbackFailedRegistration 回滚一次失败的投影注册：停用运行期并解除已建立的订阅。
// 名称占位保留到回滚结束；kept 为 true 时将 rt 原子转入 runtimes，
// 供后续 UnregisterProjection 重试清理，期间不允许同名注册取得所有权。
func (pm *ProjectionManager[ID]) rollbackFailedRegistration(
	ctx context.Context,
	name string,
	rt *projectionRuntime[ID],
	subscribed map[string]*projectionEventHandler[ID],
) (kept bool, rollbackErr error) {
	rt.deactivate()
	remaining, rollbackErr := pm.unsubscribeHandlers(ctx, name, subscribed)
	if rollbackErr != nil {
		rt.recordCleanupError(rollbackErr)
	}

	pm.mutex.Lock()
	delete(pm.registering, name)
	if rollbackErr != nil {
		rt.handlers = remaining
		pm.runtimes[name] = rt
	}
	pm.mutex.Unlock()

	return rollbackErr != nil, rollbackErr
}

// UnregisterProjection 用默认后台上下文取消注册一个投影。
func (pm *ProjectionManager[ID]) UnregisterProjection(name string) error {
	return pm.UnregisterProjectionWithContext(contextx.Background(), name)
}

// UnregisterProjectionWithContext 解除投影订阅并移除其管理状态。
func (pm *ProjectionManager[ID]) UnregisterProjectionWithContext(ctx context.Context, name string) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	pm.mutex.RLock()
	rt, exists := pm.runtimes[name]
	pm.mutex.RUnlock()
	if !exists {
		return errors.NewCode(errors.NotFound, "projection not found").
			WithContext("projection", name)
	}
	return pm.unregisterRuntime(ctx, name, rt)
}

// unregisterRuntime 解除一次具体注册：先确认该次注册仍由 manager 持有，再停用
// 运行期、等待在途事件并取消订阅。身份不匹配（该次注册已释放，或名字已被其他
// 所有者接管）时返回 NotFound，绝不按名字删除他人资源。
func (pm *ProjectionManager[ID]) unregisterRuntime(ctx context.Context, name string, rt *projectionRuntime[ID]) error {
	// 已释放的注册无需再等待；即使 ctx 已取消，释放函数仍可幂等成功。
	if current, _ := pm.runtime(name); current != rt {
		return errors.NewCode(errors.NotFound, "projection not found").
			WithContext("projection", name)
	}
	if err := rt.cleanupMu.lockContext(ctx); err != nil {
		return err
	}
	defer rt.cleanupMu.Unlock()

	pm.mutex.RLock()
	current, stillRegistered := pm.runtimes[name]
	pm.mutex.RUnlock()
	if !stillRegistered || current != rt {
		return errors.NewCode(errors.NotFound, "projection not found").
			WithContext("projection", name)
	}

	rt.deactivate()
	if err := rt.execMu.lockContext(ctx); err != nil {
		rt.recordCleanupError(err)
		return err
	}
	rt.execMu.Unlock()

	remaining, err := pm.unsubscribeHandlers(ctx, name, rt.handlers)
	if err != nil {
		rt.handlers = remaining
		rt.recordCleanupError(err)
		return err
	}

	rt.handlers = nil
	pm.mutex.Lock()
	if current, ok := pm.runtimes[name]; ok && current == rt {
		delete(pm.runtimes, name)
	}
	pm.mutex.Unlock()

	pm.logger.Info(ctx, "projection unregistered", logging.String("projection", name))
	return nil
}

// unsubscribeHandlers 释放订阅并保留失败项，供注册回滚与注销重试共用。
// 调用方保证独占 handlers；此处不持有 manager 锁。
func (pm *ProjectionManager[ID]) unsubscribeHandlers(ctx context.Context, name string, handlers map[string]*projectionEventHandler[ID]) (map[string]*projectionEventHandler[ID], error) {
	var errs []error
	remaining := make(map[string]*projectionEventHandler[ID])
	for eventType, handler := range handlers {
		if handler == nil || handler.unsub == nil {
			continue
		}
		if err := handler.unsub(ctx); err != nil {
			errs = append(errs, errors.Wrap(err, errors.Dependency, "failed to unsubscribe from event").
				WithContext("event_type", eventType).WithContext("projection", name))
			remaining[eventType] = handler
			pm.logger.Warn(ctx, "failed to unsubscribe from event", logging.Error(err),
				logging.String("event_type", eventType), logging.String("projection", name))
		}
	}
	return remaining, errors.Join(errs...)
}

// StartProjection 把指定投影状态切换为 running。
func (pm *ProjectionManager[ID]) StartProjection(name string) error {
	rt, exists := pm.runtime(name)
	if !exists {
		return errors.NewCode(errors.NotFound, "projection not found").
			WithContext("projection", name)
	}

	rt.execMu.Lock()
	defer rt.execMu.Unlock()
	if !rt.isActive() {
		return errors.NewCode(errors.Conflict, "projection is being cleaned up").
			WithContext("projection", name)
	}

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
		return errors.NewCode(errors.NotFound, "projection not found").
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
		return nil, errors.NewCode(errors.NotFound, "projection not found").
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
