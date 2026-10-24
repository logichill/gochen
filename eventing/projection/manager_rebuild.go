package projection

import (
	"context"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/upcast"
	"gochen/observe/logging"
)

// RebuildProjection 重建投影。
func (pm *ProjectionManager[ID]) RebuildProjection(ctx context.Context, name string, events []eventing.Event[ID]) error {
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

	pm.mutex.RLock()
	checkpointStore := pm.checkpointStore
	reg := pm.eventRegistry
	upgraders := pm.upgraders
	pm.mutex.RUnlock()

	pm.logger.Info(ctx, "starting projection rebuild",
		logging.String("projection", name),
		logging.Int("events", len(events)))

	for i := range events {
		evt := &events[i]
		if _, err := upcast.UpgradeEventPayload(ctx, reg, upgraders, evt); err != nil {
			if appErr, ok := err.(*errors.AppError); ok && appErr != nil {
				return appErr.Wrap("projection rebuild event payload upgrade/hydrate failed").
					WithContext("projection", name).
					WithContext("event_id", evt.GetID()).
					WithContext("event_type", evt.GetType())
			}
			return err
		}
	}

	var rebuildProjection IRebuildCheckpointingProjection[ID]
	if checkpointStore != nil && len(events) > 0 {
		var ok bool
		rebuildProjection, ok = rt.projection.(IRebuildCheckpointingProjection[ID])
		if !ok {
			return errors.NewCode(errors.Unsupported, "projection does not support checkpoint rebuild mode").
				WithContext("projection", name)
		}
	}

	_, wasRunning := rt.lifecycleState()
	rt.markRebuilding()
	defer func() {
		if rt.isRebuilding() {
			rt.markStopped()
		}
	}()

	var rebuildErr error
	rebuildPosition := int64(len(events))
	if len(events) > 0 {
		lastEvent := events[len(events)-1]
		if lastEvent.GlobalPosition > 0 {
			rebuildPosition = lastEvent.GlobalPosition
		}
	}
	if checkpointStore != nil && len(events) > 0 {
		lastEvent := events[len(events)-1]
		position := rebuildPosition
		checkpoint := NewCheckpoint(
			name,
			position,
			lastEvent.ID,
			lastEvent.Timestamp,
		)
		rebuildErr = rebuildProjection.RebuildWithCheckpoint(ctx, events, newRebuildCheckpointStore(checkpointStore), checkpoint)
	} else {
		rebuildErr = rt.projection.Rebuild(ctx, events)
	}

	// 重建批量完成后补记事件量指标：重建无法逐事件插性测耗时（事件切片由
	// 投影内部消费），仅在总量与成功/失败上补齐观测。
	if metrics := pm.getMetrics(); metrics != nil && len(events) > 0 {
		for i := range events {
			var lag time.Duration
			if ts := events[i].Timestamp; !ts.IsZero() {
				lag = time.Since(ts)
				if lag < 0 {
					lag = 0
				}
			}
			metrics.RecordProjectionUpdate(rebuildErr == nil, lag)
		}
	}

	if rebuildErr != nil {
		rt.markError(rebuildErr)
		return errors.Wrap(rebuildErr, errors.Internal, "failed to rebuild projection").
			WithContext("projection", name)
	}
	if checkpointStore != nil && len(events) == 0 {
		if err := checkpointStore.Delete(ctx, name); err != nil {
			wrapped := errors.Wrap(err, errors.Database, "failed to delete checkpoint after empty rebuild").
				WithContext("projection", name)
			rt.markError(wrapped)
			return wrapped
		}
	}

	rt.updateAfterRebuild(events, rebuildPosition)
	if wasRunning {
		rt.markRunning()
	}

	pm.logger.Info(ctx, "projection rebuild completed",
		logging.String("projection", name),
		logging.Int("events", len(events)))
	return nil
}
