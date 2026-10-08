package outbox

import (
	"context"
	stderrors "errors"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/observe/logging"
	"gochen/process/task"
)

type outboxPublisherCore[ID comparable] struct {
	repo       IOutboxRepository[ID]
	bus        bus.IEventBus
	cfg        OutboxConfig
	log        logging.ILogger
	metrics    IPublisherMetricsRecorder
	supervisor *task.TaskSupervisor

	eventRegistry *registry.Registry
	upgraders     *upcast.UpgraderRegistry
	dlq           IDLQRepository[ID]
}

type outboxPublishResult struct {
	stage        outboxFailureStage
	published    bool
	err          error
	keepaliveErr *claimKeepaliveError
}

// outboxMarkStrategy 隔离串行直写与并行批量回写差异；单条 entry 的发布状态机由 core 统一编排。
type outboxMarkStrategy[ID comparable] struct {
	markPublished func(context.Context, OutboxEntry[ID]) error
	markFailed    func(context.Context, outboxFailureMark[ID]) error
}

type outboxFailureMark[ID comparable] struct {
	entry      OutboxEntry[ID]
	entryID    int64
	claimToken string
	errorMsg   string
	nextRetry  time.Time
	moveToDLQ  bool
	dlqEntry   OutboxEntry[ID]
}

func (c outboxPublisherCore[ID]) logger() logging.ILogger {
	if c.log == nil {
		return logging.NewNoopLogger()
	}
	return c.log
}

func validatePublisherCodecs(reg *registry.Registry, upgraders *upcast.UpgraderRegistry) error {
	if reg == nil {
		return errors.NewCode(errors.InvalidInput, "event registry cannot be nil")
	}
	if upgraders == nil {
		return errors.NewCode(errors.InvalidInput, "event upgrader registry cannot be nil")
	}
	return nil
}

func (c outboxPublisherCore[ID]) claimPending(ctx context.Context) ([]OutboxEntry[ID], error) {
	if err := validatePublisherDependencies(c.repo, c.bus); err != nil {
		return nil, err
	}
	return c.repo.ClaimPendingEntries(ctx, c.cfg.BatchSize)
}

func (c outboxPublisherCore[ID]) cleanupPublished(ctx context.Context, now time.Time) error {
	return c.repo.DeletePublished(ctx, now.Add(-c.cfg.RetentionPeriod))
}

func (c outboxPublisherCore[ID]) processClaimed(
	ctx context.Context,
	entry OutboxEntry[ID],
	strategy outboxMarkStrategy[ID],
) error {
	result := c.publishClaimed(ctx, entry)
	log := c.logger()
	var reportErr error
	if result.keepaliveErr != nil {
		log.Error(ctx, "outbox claim keepalive failed",
			logging.Int64("entry_id", entry.ID),
			logging.String("event_type", entry.EventType),
			logging.Error(result.keepaliveErr))
		reportErr = result.keepaliveErr
	}

	if result.err != nil && result.keepaliveErr == nil {
		log.Warn(ctx, result.stage.warnLogMessage(),
			logging.Int64("entry_id", entry.ID),
			logging.String("event_type", entry.EventType),
			logging.Error(result.err))
		if strategy.markFailed == nil {
			return errors.Join(reportErr, errors.NewCode(errors.FailedPrecondition, "outbox failure mark strategy is missing"))
		}
		parent := context.Background()
		if ctx != nil {
			parent = context.WithoutCancel(ctx)
		}
		markCtx, cancel := context.WithTimeout(parent, defaultParallelPublisherStopTimeout)
		defer cancel()
		markErr := strategy.markFailed(markCtx, c.buildFailureMark(entry, result.err.Error()))
		return errors.Join(reportErr, markErr)
	}

	if !result.published {
		return reportErr
	}
	if strategy.markPublished == nil {
		return errors.Join(reportErr, errors.NewCode(errors.FailedPrecondition, "outbox published mark strategy is missing"))
	}
	if err := strategy.markPublished(ctx, entry); err != nil {
		return errors.Join(reportErr, err)
	}
	log.Debug(ctx, "event published successfully",
		logging.Int64("entry_id", entry.ID),
		logging.String("event_type", entry.EventType))
	return reportErr
}

func (c outboxPublisherCore[ID]) publishClaimed(ctx context.Context, entry OutboxEntry[ID]) outboxPublishResult {
	stage := outboxFailureDeserialize
	if err := validatePublisherDependencies(c.repo, c.bus); err != nil {
		return outboxPublishResult{stage: outboxFailurePublish, err: err}
	}
	if err := validatePublisherCodecs(c.eventRegistry, c.upgraders); err != nil {
		return outboxPublishResult{stage: stage, err: err}
	}

	var evt eventing.Event[ID]
	var published bool
	err := runWithClaimKeepalive(ctx, c.supervisor, c.repo, entry, c.cfg.ClaimLease, c.cfg.ClaimRenewInterval, func(processCtx context.Context) error {
		decodeStart := time.Now()
		var decodeErr error
		evt, decodeErr = entry.toEventWithContext(processCtx, c.eventRegistry, c.upgraders)
		if c.metrics != nil {
			c.metrics.RecordOutboxDecode(time.Since(decodeStart), decodeErr != nil)
		}
		if decodeErr != nil {
			return decodeErr
		}

		stage = outboxFailurePublish
		publishStart := time.Now()
		publishErr := c.bus.PublishEvent(processCtx, &evt)
		if c.metrics != nil {
			c.metrics.RecordOutboxPublish(time.Since(publishStart), publishErr != nil)
		}
		if publishErr == nil {
			published = true
		}
		return publishErr
	})

	var keepaliveErr *claimKeepaliveError
	if stderrors.As(err, &keepaliveErr) {
		return outboxPublishResult{stage: stage, published: published, err: keepaliveErr, keepaliveErr: keepaliveErr}
	}
	return outboxPublishResult{stage: stage, published: published, err: err}
}

func (c outboxPublisherCore[ID]) markPublishedWithRecovery(ctx context.Context, entry OutboxEntry[ID]) error {
	claimed := ClaimedEntry{ID: entry.ID, ClaimToken: entry.ClaimToken}
	markErr := c.markPublishedAfterSuccessfulPublish(ctx, claimed, c.cfg.MarkPublishedAttempts)
	if markErr == nil {
		return nil
	}
	c.logger().Error(ctx, "recover published entry after mark failure",
		logging.Int64("entry_id", entry.ID),
		logging.Error(markErr))
	return c.recoverPublishedMarkFailure(ctx, entry, markErr)
}

func (c outboxPublisherCore[ID]) markFailureDirect(ctx context.Context, mark outboxFailureMark[ID]) error {
	if handled, markErr := c.markFailedAndMoveToDLQ(ctx, mark); handled {
		if markErr == nil {
			return nil
		}
		c.logger().Warn(ctx, "atomic DLQ move failed; falling back to non-atomic recovery",
			logging.Int64("entry_id", mark.entryID),
			logging.Error(markErr))
	}
	if err := c.markFailed(ctx, mark); err != nil {
		return err
	}
	return c.moveFailureToDLQ(ctx, mark)
}

func (c outboxPublisherCore[ID]) markPublishedAfterSuccessfulPublish(ctx context.Context, entry ClaimedEntry, attempts int) error {
	return c.markPublishedAfterSuccessfulPublishWithRetryContext(ctx, entry, attempts, nil)
}

func (c outboxPublisherCore[ID]) markPublishedAfterSuccessfulPublishWithinContext(ctx context.Context, entry ClaimedEntry, attempts int) error {
	return c.markPublishedAfterSuccessfulPublishWithRetryContext(ctx, entry, attempts, ctx)
}

func (c outboxPublisherCore[ID]) markPublishedAfterSuccessfulPublishWithRetryContext(ctx context.Context, entry ClaimedEntry, attempts int, retryCtx context.Context) error {
	if attempts < 1 {
		attempts = 1
	}

	log := c.logger()
	var lastErr error
	if err := c.repo.MarkAsPublished(ctx, entry.ID, entry.ClaimToken); err == nil {
		return nil
	} else {
		lastErr = err
		log.Error(ctx, "mark as published failed after successful publish",
			logging.Int64("entry_id", entry.ID),
			logging.Int("attempt", 1),
			logging.Error(err))
	}

	if attempts == 1 {
		return errors.Wrap(lastErr, errors.Dependency, "mark as published failed after successful publish").
			WithContext("outbox_entry_id", entry.ID)
	}
	cancel := func() {}
	if retryCtx == nil {
		retryCtx, cancel = context.WithTimeout(context.Background(), defaultParallelPublisherStopTimeout)
	}
	defer cancel()
	for attempt := 2; attempt <= attempts; attempt++ {
		if retryCtx.Err() != nil {
			lastErr = retryCtx.Err()
			log.Error(retryCtx, "mark as published retry context expired",
				logging.Int64("entry_id", entry.ID),
				logging.Error(retryCtx.Err()))
			break
		}
		if err := c.repo.MarkAsPublished(retryCtx, entry.ID, entry.ClaimToken); err == nil {
			return nil
		} else {
			lastErr = err
			log.Error(retryCtx, "mark as published retry failed after successful publish",
				logging.Int64("entry_id", entry.ID),
				logging.Int("attempt", attempt),
				logging.Error(err))
		}
	}
	if lastErr == nil {
		lastErr = errors.NewCode(errors.Dependency, "mark as published failed after successful publish")
	}
	return errors.Wrap(lastErr, errors.Dependency, "mark as published failed after successful publish").
		WithContext("outbox_entry_id", entry.ID)
}

func (c outboxPublisherCore[ID]) buildFailureMark(entry OutboxEntry[ID], errorMsg string) outboxFailureMark[ID] {
	dlqEntry := entry
	dlqEntry.Status = OutboxStatusFailed
	dlqEntry.ClaimToken = ""
	dlqEntry.LeaseUntil = nil
	dlqEntry.RetryCount = entry.RetryCount + 1
	dlqEntry.LastError = errorMsg
	nextRetry := entry.CalculateNextRetryTime(c.cfg.RetryInterval)
	dlqEntry.NextRetryAt = &nextRetry

	return outboxFailureMark[ID]{
		entry:      entry,
		entryID:    entry.ID,
		claimToken: entry.ClaimToken,
		errorMsg:   errorMsg,
		nextRetry:  nextRetry,
		moveToDLQ:  c.dlq != nil && entry.RetryCount+1 >= c.cfg.MaxRetries,
		dlqEntry:   dlqEntry,
	}
}

func (c outboxPublisherCore[ID]) markFailed(ctx context.Context, mark outboxFailureMark[ID]) error {
	return c.repo.MarkAsFailed(ctx, mark.entryID, mark.claimToken, mark.errorMsg, mark.nextRetry)
}

func (c outboxPublisherCore[ID]) recoverPublishedMarkFailure(ctx context.Context, entry OutboxEntry[ID], markPublishedErr error) error {
	if markPublishedErr == nil {
		return nil
	}
	mark := c.buildFailureMark(entry, markPublishedErr.Error())
	parent := context.Background()
	if ctx != nil {
		parent = context.WithoutCancel(ctx)
	}
	recoverCtx, cancel := context.WithTimeout(parent, defaultParallelPublisherStopTimeout)
	defer cancel()
	if err := c.markFailed(recoverCtx, mark); err != nil {
		return errors.Wrap(err, errors.Dependency, "recover published mark failure failed").
			WithContext("outbox_entry_id", entry.ID)
	}
	c.logger().Warn(ctx, "outbox published entry marked failed after mark failure",
		logging.Int64("entry_id", entry.ID),
		logging.Error(markPublishedErr))
	return errors.Wrap(markPublishedErr, errors.Dependency, "mark published failed after event was published").
		WithContext("outbox_entry_id", entry.ID)
}

func (c outboxPublisherCore[ID]) markFailedAndMoveToDLQ(ctx context.Context, mark outboxFailureMark[ID]) (bool, error) {
	if c.dlq == nil || !mark.moveToDLQ {
		return false, nil
	}
	atomic, ok := c.dlq.(IAtomicDLQMover[ID])
	if !ok {
		return false, nil
	}
	if err := atomic.MarkFailedAndMoveToDLQ(ctx, mark.entry, mark.claimToken, mark.errorMsg); err != nil {
		return true, errors.Wrap(err, errors.Dependency, "mark failed and move to DLQ failed").
			WithContext("outbox_entry_id", mark.entryID)
	}
	return true, nil
}

func (c outboxPublisherCore[ID]) moveFailureToDLQ(ctx context.Context, mark outboxFailureMark[ID]) error {
	if c.dlq == nil || !mark.moveToDLQ {
		return nil
	}
	if err := c.dlq.MoveToDLQ(ctx, mark.dlqEntry); err != nil {
		return errors.Wrap(err, errors.Dependency, "move to DLQ failed").
			WithContext("outbox_entry_id", mark.entryID)
	}
	return nil
}
