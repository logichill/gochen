package projection

import (
	"context"
	"time"

	gerrors "gochen/errors"
	"gochen/eventing"
	"gochen/eventing/upcast"
	"gochen/logging"
)

type applyEventCommonOptions struct {
	requireRunning          bool
	enableRetry             bool
	allowCheckpoint         bool
	clearLastErrorOnSuccess bool
	upgradeLogMessage       string
}

type applyEventCommonResult struct {
	skipped         bool
	handleDuration  time.Duration
	processedEvents int64
	failedEvents    int64
}

// applyEventCommon 应用事件Common。
func (pm *ProjectionManager[ID]) applyEventCommon(
	ctx context.Context,
	rt *projectionRuntime[ID],
	evt eventing.IEvent,
	opts applyEventCommonOptions,
) (applyEventCommonResult, error) {
	var res applyEventCommonResult
	if pm == nil || rt == nil || rt.projection == nil || evt == nil {
		res.skipped = true
		return res, nil
	}
	projectionName := rt.projection.Name()

	pm.mutex.RLock()
	checkpointStore := pm.checkpointStore
	reg := pm.eventRegistry
	upgraders := pm.upgraders
	pm.mutex.RUnlock()

	processedBefore := rt.processedEvents()
	saveCheckpointOnSuccess := opts.allowCheckpoint && checkpointStore != nil && pm.shouldSaveCheckpointAfterEvent(rt)

	if opts.requireRunning && !rt.isRunning() {
		res.skipped = true
		return res, nil
	}

	if e, ok := evt.(*eventing.Event[ID]); ok && e != nil {
		if _, uerr := upcast.UpgradeEventPayload(ctx, reg, upgraders, e); uerr != nil {
			msg := opts.upgradeLogMessage
			if msg == "" {
				msg = "projection event payload upgrade/hydrate failed"
			}
			if appErr, ok := uerr.(*gerrors.AppError); ok && appErr != nil {
				return res, appErr.Wrap(msg).
					WithContext("projection", projectionName).
					WithContext("event_id", e.GetID()).
					WithContext("event_type", e.GetType())
			}
			return res, uerr
		}
	}

	var err error
	nextCursor := NewCheckpoint(
		projectionName,
		processedBefore+1,
		evt.GetID(),
		evt.GetTimestamp(),
	)
	handle := func(handleCtx context.Context) error {
		if saveCheckpointOnSuccess {
			cpProjection, ok := rt.projection.(ICheckpointingProjection[ID])
			if !ok {
				return gerrors.NewCode(gerrors.Unsupported, "projection does not support checkpoint mode").
					WithContext("projection", projectionName)
			}
			return cpProjection.HandleWithCheckpoint(handleCtx, evt, checkpointStore, nextCursor)
		}
		return rt.projection.Handle(handleCtx, evt)
	}

	handleStart := time.Now()
	if opts.enableRetry {
		var aborted bool
		err, aborted = pm.handleWithRetry(ctx, projectionName, evt, handle)
		if aborted {
			res.handleDuration = time.Since(handleStart)
			return res, err
		}
	} else {
		err = handle(ctx)
	}
	res.handleDuration = time.Since(handleStart)

	recorded := rt.recordApplyResult(evt, err, opts.clearLastErrorOnSuccess, nextCursor, saveCheckpointOnSuccess)
	recorded.handleDuration = res.handleDuration
	res = recorded

	if err != nil {
		return res, err
	}
	return res, nil
}

func (pm *ProjectionManager[ID]) handleWithRetry(
	ctx context.Context,
	projectionName string,
	evt eventing.IEvent,
	handle func(context.Context) error,
) (err error, aborted bool) {
	// 重放阶段的重试：仅在 ResumeFromCheckpoint/replay 中生效，避免影响在线事件总线语义。
	maxRetries := 0
	backoff := time.Duration(0)
	if pm != nil && pm.config != nil {
		if pm.config.MaxRetries > 0 {
			maxRetries = pm.config.MaxRetries
		}
		backoff = pm.config.RetryBackoff
	}

	var backoffTimer *time.Timer
	if backoff > 0 {
		// Reuse a timer to avoid per-retry allocation from time.After in retry loops.
		backoffTimer = time.NewTimer(0)
		if !backoffTimer.Stop() {
			select {
			case <-backoffTimer.C:
			default:
			}
		}
		defer backoffTimer.Stop()
	}

	for attempt := 0; ; attempt++ {
		err = handle(ctx)
		if err == nil {
			return nil, false
		}

		// 已达到最大重试次数（attempt 表示已进行的重试次数）
		if attempt >= maxRetries {
			return err, false
		}

		pm.logger.Warn(ctx, "projection replay event retry",
			logging.String("projection", projectionName),
			logging.String("event_id", evt.GetID()),
			logging.Int("attempt", attempt+1), // retry attempt number (starting from 1)
			logging.Error(err),
		)

		if backoff > 0 {
			if !backoffTimer.Stop() {
				select {
				case <-backoffTimer.C:
				default:
				}
			}
			backoffTimer.Reset(backoff)
			select {
			case <-ctx.Done():
				return ctx.Err(), true
			case <-backoffTimer.C:
			}
		}
	}
}
