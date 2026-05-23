package projection

import (
	"context"
	"time"

	gerrors "gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/logging"
)

// ResumeFromCheckpoint 从检查点恢复投影。
//
// 说明：
// - 加载检查点并从上次位置继续处理事件。
// - 如果检查点不存在，则从头开始。
func (pm *ProjectionManager[ID]) ResumeFromCheckpoint(ctx context.Context, projectionName string) error {
	rt, exists := pm.runtime(projectionName)
	if !exists {
		return gerrors.NewCode(gerrors.NotFound, "projection not found").
			WithContext("projection", projectionName)
	}
	return pm.resumeRuntimeFromCheckpoint(ctx, rt, true)
}

func (pm *ProjectionManager[ID]) resumeRuntimeFromCheckpoint(ctx context.Context, rt *projectionRuntime[ID], startWhenDone bool) error {
	if rt == nil || rt.projection == nil {
		return gerrors.NewCode(gerrors.NotFound, "projection not found")
	}
	projectionName := rt.projection.Name()

	rt.execMu.Lock()
	defer rt.execMu.Unlock()
	wasRunning := rt.isRunning()
	if !startWhenDone && !wasRunning {
		return nil
	}

	pm.mutex.RLock()
	checkpointStore := pm.checkpointStore
	eventStore := pm.eventStore
	pm.mutex.RUnlock()

	if checkpointStore == nil {
		pm.logger.Warn(ctx, "checkpoint store not configured, skipping recovery",
			logging.String("projection", projectionName))
		if startWhenDone {
			rt.markRunning()
		}
		return nil
	}

	checkpoint := rt.cursorSnapshot()
	if startWhenDone || checkpoint == nil {
		durable, err := pm.loadCheckpoint(ctx, checkpointStore, projectionName)
		if err != nil {
			return err
		}
		if shouldUseDurableCheckpoint(checkpoint, durable) {
			checkpoint = durable
			rt.prefillFromCheckpoint(checkpoint)
		}
	}
	if checkpoint == nil {
		checkpoint = NewCheckpoint(projectionName, 0, "", time.Time{})
		rt.prefillFromCheckpoint(checkpoint)
	}

	pm.logger.Info(ctx, "resuming projection from checkpoint",
		logging.String("projection", projectionName),
		logging.Int64("position", checkpoint.Position),
		logging.String("last_event_id", checkpoint.LastEventID))

	if eventStore == nil {
		pm.logger.Warn(ctx, "event store not configured, cannot replay from checkpoint, starting directly",
			logging.String("projection", projectionName))
		if startWhenDone {
			rt.markRunning()
		}
		return nil
	}

	replayed, err := pm.replayProjectionFromCheckpoint(ctx, rt, checkpoint)
	if err != nil {
		// 约定：事件存储在 cursor 不存在时返回 errors.NotFound；
		// 这里仅在“checkpoint 有游标”的场景下将 NotFound 视为 checkpoint gap。
		return err
	}

	pm.logger.Info(ctx, "resumed from checkpoint and completed history replay",
		logging.String("projection", projectionName),
		logging.Int64("replayed_events", replayed))

	if startWhenDone || wasRunning {
		rt.markRunning()
	}
	return nil
}

func (pm *ProjectionManager[ID]) loadCheckpoint(ctx context.Context, checkpointStore ICheckpointStore, projectionName string) (*Checkpoint, error) {
	checkpoint, err := checkpointStore.Load(ctx, projectionName)
	if err != nil {
		if gerrors.Is(err, gerrors.NotFound) {
			pm.logger.Info(ctx, "checkpoint not found, starting from beginning",
				logging.String("projection", projectionName))
			return NewCheckpoint(projectionName, 0, "", time.Time{}), nil
		}
		return nil, gerrors.Wrap(err, gerrors.Database, "failed to load checkpoint").
			WithContext("projection", projectionName)
	}
	return checkpoint, nil
}

func shouldUseDurableCheckpoint(runtimeCursor, durable *Checkpoint) bool {
	if durable == nil {
		return false
	}
	if runtimeCursor == nil {
		return true
	}
	return durable.Position > runtimeCursor.Position
}

func (pm *ProjectionManager[ID]) replayProjectionFromCheckpoint(ctx context.Context, rt *projectionRuntime[ID], checkpoint *Checkpoint) (int64, error) {
	projection := rt.projection
	projectionName := projection.Name()
	supported := make(map[string]struct{})
	supportedTypes := projection.SupportedEventTypes()
	for _, t := range supportedTypes {
		supported[t] = struct{}{}
	}

	lastEventID := checkpoint.LastEventID
	fromTime := checkpoint.LastEventTime
	var replayed int64

	for {
		events, hasMore, err := pm.fetchEventsForReplay(ctx, lastEventID, fromTime, supportedTypes)
		if err != nil {
			// 将 “cursor not found” 作为 NotFound 传播；上层会结合 checkpoint.LastEventID 决策策略。
			if appErr, ok := err.(*gerrors.AppError); ok && appErr != nil {
				return replayed, appErr.WithContext("projection", projectionName)
			}
			return replayed, gerrors.Wrap(err, gerrors.Database, "failed to load events for projection").
				WithContext("projection", projectionName)
		}
		if len(events) == 0 {
			if hasMore {
				continue
			}
			break
		}

		for i := range events {
			evt := &events[i]
			if len(supported) > 0 {
				if _, ok := supported[evt.GetType()]; !ok {
					continue
				}
			}

			if err := pm.applyReplayEvent(ctx, rt, evt); err != nil {
				rt.markError(err)

				return replayed, gerrors.Wrap(err, gerrors.Internal, "replay projection failed").
					WithContext("projection", projectionName).
					WithContext("event_id", evt.GetID())
			}

			replayed++
			lastEventID = evt.GetID()
			fromTime = evt.GetTimestamp()
		}

		if !hasMore {
			break
		}
	}

	return replayed, nil
}

func (pm *ProjectionManager[ID]) fetchEventsForReplay(ctx context.Context, after string, fromTime time.Time, supportedTypes []string) ([]eventing.Event[ID], bool, error) {
	stream, err := pm.eventStore.StreamEvents(ctx, &store.StreamOptions{
		After:    after,
		FromTime: fromTime,
		Types:    supportedTypes,
		Limit:    replayBatchLimit,
	})
	if err != nil {
		return nil, false, err
	}
	if stream == nil {
		return nil, false, nil
	}
	return stream.Events, stream.HasMore, nil
}

// applyReplayEvent 应用Replay事件。
func (pm *ProjectionManager[ID]) applyReplayEvent(ctx context.Context, rt *projectionRuntime[ID], evt eventing.IEvent) error {
	_, err := pm.applyEventCommon(ctx, rt, evt, applyEventCommonOptions{
		requireRunning:          false,
		enableRetry:             true,
		allowCheckpoint:         true,
		clearLastErrorOnSuccess: true,
		upgradeLogMessage:       "projection replay event payload upgrade/hydrate failed",
	})
	return err
}

// ResumeAllFromCheckpoint 从检查点恢复所有投影。
//
// 说明：
// - 批量恢复所有已注册的投影。
func (pm *ProjectionManager[ID]) ResumeAllFromCheckpoint(ctx context.Context) error {
	pm.mutex.RLock()
	names := make([]string, 0, len(pm.runtimes))
	for name := range pm.runtimes {
		names = append(names, name)
	}
	pm.mutex.RUnlock()

	var errs []error

	for _, name := range names {
		if err := pm.ResumeFromCheckpoint(ctx, name); err != nil {
			pm.logger.Error(ctx, "failed to resume projection", logging.Error(err),
				logging.String("projection", name))
			if appErr, ok := err.(*gerrors.AppError); ok && appErr != nil {
				errs = append(errs, appErr.WithContext("projection", name))
			} else {
				errs = append(errs, gerrors.Wrap(err, gerrors.Internal, "failed to resume projection").WithContext("projection", name))
			}
			// 继续尝试恢复其他投影
		}
	}

	if len(errs) > 0 {
		return gerrors.Join(errs...)
	}
	return nil
}
