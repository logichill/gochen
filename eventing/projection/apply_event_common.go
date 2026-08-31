package projection

import (
	"context"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/upcast"
	"gochen/observe/logging"
	"gochen/policy/retry"
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
			if appErr, ok := uerr.(*errors.AppError); ok && appErr != nil {
				return res, appErr.Wrap(msg).
					WithContext("projection", projectionName).
					WithContext("event_id", e.GetID()).
					WithContext("event_type", e.GetType())
			}
			return res, uerr
		}
	}

	var err error
	nextPosition := processedBefore + 1
	if positioned, ok := evt.(interface{ GetGlobalPosition() int64 }); ok {
		if pos := positioned.GetGlobalPosition(); pos > 0 {
			nextPosition = pos
		}
	}
	nextCursor := NewCheckpoint(
		projectionName,
		nextPosition,
		evt.GetID(),
		evt.GetTimestamp(),
	)
	handle := func(handleCtx context.Context) error {
		if saveCheckpointOnSuccess {
			cpProjection, ok := rt.projection.(ICheckpointingProjection[ID])
			if !ok {
				return errors.NewCode(errors.Unsupported, "projection does not support checkpoint mode").
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

	cfg := retry.Config{
		InitialDelay:  backoff,
		BackoffFactor: 1,
		MaxDelay:      backoff,
		JitterRatio:   0,
	}
	var retryTimer *time.Timer
	defer func() {
		if retryTimer == nil || retryTimer.Stop() {
			return
		}
		select {
		case <-retryTimer.C:
		default:
		}
	}()
	for attempt := 0; ; attempt++ {
		err = handle(ctx)
		if err == nil {
			return nil, false
		}
		if attempt >= maxRetries {
			return err, false
		}

		retryAttempt := attempt + 1
		pm.logger.Warn(ctx, "projection replay event retry",
			logging.String("projection", projectionName),
			logging.String("event_id", evt.GetID()),
			logging.Int("attempt", retryAttempt),
			logging.Error(err),
		)

		delay := retry.ComputeDelay(cfg, retryAttempt)
		if delay <= 0 {
			continue
		}
		if retryTimer == nil {
			retryTimer = time.NewTimer(delay)
		} else {
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			retryTimer.Reset(delay)
		}
		select {
		case <-retryTimer.C:
		case <-ctx.Done():
			return ctx.Err(), true
		}
	}
}
