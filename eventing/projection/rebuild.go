package projection

import (
	"context"

	gerrors "gochen/errors"
	"gochen/eventing"
	"gochen/eventing/upcast"
	"gochen/logging"
)

// RebuildProjection 重建投影。
func (pm *ProjectionManager[ID]) RebuildProjection(ctx context.Context, name string, events []eventing.Event[ID]) error {
	rt, exists := pm.runtime(name)
	if !exists {
		return gerrors.NewCode(gerrors.NotFound, "projection not found").
			WithContext("projection", name)
	}

	rt.execMu.Lock()
	defer rt.execMu.Unlock()

	pm.mutex.Lock()
	checkpointStore := pm.checkpointStore
	reg := pm.eventRegistry
	upgraders := pm.upgraders
	pm.mutex.Unlock()

	pm.logger.Info(ctx, "starting projection rebuild",
		logging.String("projection", name),
		logging.Int("events", len(events)))

	for i := range events {
		evt := &events[i]
		if _, err := upcast.UpgradeEventPayload(ctx, reg, upgraders, evt); err != nil {
			if appErr, ok := err.(*gerrors.AppError); ok && appErr != nil {
				return appErr.Wrap("projection rebuild event payload upgrade/hydrate failed").
					WithContext("projection", name).
					WithContext("event_id", evt.GetID()).
					WithContext("event_type", evt.GetType())
			}
			return err
		}
	}

	// 清空检查点（如果已配置）
	if checkpointStore != nil {
		if err := checkpointStore.Delete(ctx, name); err != nil {
			return gerrors.Wrap(err, gerrors.Database, "failed to delete checkpoint before rebuild").
				WithContext("projection", name)
		}
	}

	rt.markRebuilding()

	var rebuildErr error
	if checkpointStore != nil && len(events) > 0 {
		lastEvent := events[len(events)-1]
		checkpoint := NewCheckpoint(
			name,
			int64(len(events)),
			lastEvent.ID,
			lastEvent.Timestamp,
		)
		rebuildProjection, ok := rt.projection.(IRebuildCheckpointingProjection[ID])
		if !ok {
			return gerrors.NewCode(gerrors.Unsupported, "projection does not support checkpoint rebuild mode").
				WithContext("projection", name)
		}
		rebuildErr = rebuildProjection.RebuildWithCheckpoint(ctx, events, checkpointStore, checkpoint)
	} else {
		rebuildErr = rt.projection.Rebuild(ctx, events)
	}

	if rebuildErr != nil {
		rt.markError(rebuildErr)
		return gerrors.Wrap(rebuildErr, gerrors.Internal, "failed to rebuild projection").
			WithContext("projection", name)
	}

	rt.updateAfterRebuild(events)

	pm.logger.Info(ctx, "projection rebuild completed",
		logging.String("projection", name),
		logging.Int("events", len(events)))
	return nil
}
