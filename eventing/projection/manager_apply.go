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
		// 多个投影可能并发消费同一个事件指针；先浅拷贝副本再升级，
		// 避免对共享事件的 Payload/SchemaVersion 原地修改引发 Data Race。
		copyEvent := *e
		if _, uerr := upcast.UpgradeEventPayload(ctx, reg, upgraders, &copyEvent); uerr != nil {
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
		// 升级后的副本必须回赋给 evt，否则后续处理器仍拿到未升级的原始事件，
		// Payload/SchemaVersion 的升级结果被静默丢弃。
		evt = &copyEvent
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
		if opts.allowCheckpoint && checkpointStore != nil {
			cpProjection, cpOK := rt.projection.(ICheckpointingProjection[ID])
			if saveCheckpointOnSuccess {
				// 保存位点的边界事件：读模型写入与 checkpoint 保存同事务原子提交。
				if !cpOK {
					return errors.NewCode(errors.Unsupported, "projection does not support checkpoint mode").
						WithContext("projection", projectionName)
				}
				return cpProjection.HandleWithCheckpoint(handleCtx, evt, checkpointStore, nextCursor)
			}
			if cpOK {
				// 批量检查点模式下的中间事件：不落持久化位点，但仍包裹
				// 单事件事务，保证读模型写入本身的全有或全无；否则中间
				// 事件完全脱离事务边界，崩溃后重放会留下半提交状态。
				// checkpoint 传 nil 表示封装层只提交业务写入，不触发位点保存。
				return cpProjection.HandleWithCheckpoint(handleCtx, evt, checkpointStore, nil)
			}
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

	// 指标上报统一在公共路径：在线处理（HandlerEvent）、重放（Replay）与
	// 重建（Rebuild 复用 applyReplayEvent 的路径）都能被监控观测；此前仅
	// 在线路径打点，重放/重建期间大盘零数据。
	if metrics := pm.getMetrics(); metrics != nil {
		lag := time.Duration(0)
		if ts := evt.GetTimestamp(); !ts.IsZero() {
			lag = time.Since(ts)
			if lag < 0 {
				lag = 0
			}
		}
		metrics.RecordEventProcessed(res.handleDuration, err == nil)
		metrics.RecordProjectionUpdate(err == nil, lag)
	}

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
