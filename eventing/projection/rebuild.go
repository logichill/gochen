package projection

import (
	"context"

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

	rt.markRebuilding()

	var rebuildErr error
	if checkpointStore != nil && len(events) > 0 {
		lastEvent := events[len(events)-1]
		position := int64(len(events))
		if lastEvent.GlobalPosition > 0 {
			position = lastEvent.GlobalPosition
		}
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

	rt.updateAfterRebuild(events)

	pm.logger.Info(ctx, "projection rebuild completed",
		logging.String("projection", name),
		logging.Int("events", len(events)))
	return nil
}
