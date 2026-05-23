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
	"gochen/logging"
)

type outboxPublisherCore[ID comparable] struct {
	repo    IOutboxRepository[ID]
	bus     bus.IEventBus
	cfg     OutboxConfig
	log     logging.ILogger
	metrics IPublisherMetricsRecorder

	eventRegistry *registry.Registry
	upgraders     *upcast.UpgraderRegistry
	dlq           IDLQRepository[ID]
}

type outboxPublishResult struct {
	stage        outboxFailureStage
	err          error
	keepaliveErr *claimKeepaliveError
}

type outboxFailureMark[ID comparable] struct {
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

func (c outboxPublisherCore[ID]) publishClaimed(ctx context.Context, entry OutboxEntry[ID]) outboxPublishResult {
	stage := outboxFailureDeserialize
	if err := validatePublisherCodecs(c.eventRegistry, c.upgraders); err != nil {
		return outboxPublishResult{stage: stage, err: err}
	}

	var evt eventing.Event[ID]
	err := runWithClaimKeepalive(ctx, c.repo, entry, c.cfg.ClaimLease, c.cfg.ClaimRenewInterval, func(processCtx context.Context) error {
		decodeStart := time.Now()
		var decodeErr error
		evt, decodeErr = entry.ToEventWith(c.eventRegistry, c.upgraders)
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
		return publishErr
	})

	var keepaliveErr *claimKeepaliveError
	if stderrors.As(err, &keepaliveErr) {
		return outboxPublishResult{stage: stage, err: keepaliveErr, keepaliveErr: keepaliveErr}
	}
	return outboxPublishResult{stage: stage, err: err}
}

func (c outboxPublisherCore[ID]) markPublishedAfterSuccessfulPublish(ctx context.Context, entry ClaimedEntry, attempts int) bool {
	if attempts < 1 {
		attempts = 1
	}

	log := c.logger()
	if err := c.repo.MarkAsPublished(ctx, entry.ID, entry.ClaimToken); err == nil {
		return true
	} else {
		log.Error(ctx, "mark as published failed after successful publish",
			logging.Int64("entry_id", entry.ID),
			logging.Int("attempt", 1),
			logging.Error(err))
	}

	if attempts == 1 {
		return false
	}
	// 事件已成功发布后，成功标记需要 best-effort 完成；即使调用方 ctx 已取消，
	// 也继续使用独立超时上下文重试，降低后续重复发布概率。
	retryCtx, cancel := context.WithTimeout(context.Background(), defaultParallelPublisherStopTimeout)
	defer cancel()
	for attempt := 2; attempt <= attempts; attempt++ {
		if retryCtx.Err() != nil {
			log.Error(context.Background(), "mark as published retry context expired",
				logging.Int64("entry_id", entry.ID),
				logging.Error(retryCtx.Err()))
			return false
		}
		if err := c.repo.MarkAsPublished(retryCtx, entry.ID, entry.ClaimToken); err == nil {
			return true
		} else {
			log.Error(retryCtx, "mark as published retry failed after successful publish",
				logging.Int64("entry_id", entry.ID),
				logging.Int("attempt", attempt),
				logging.Error(err))
		}
	}
	return false
}

func (c outboxPublisherCore[ID]) buildFailureMark(entry OutboxEntry[ID], errorMsg string) outboxFailureMark[ID] {
	dlqEntry := entry
	dlqEntry.RetryCount = entry.RetryCount + 1
	dlqEntry.LastError = errorMsg

	return outboxFailureMark[ID]{
		entryID:    entry.ID,
		claimToken: entry.ClaimToken,
		errorMsg:   errorMsg,
		nextRetry:  entry.CalculateNextRetryTime(c.cfg.RetryInterval),
		moveToDLQ:  c.dlq != nil && entry.RetryCount+1 >= c.cfg.MaxRetries,
		dlqEntry:   dlqEntry,
	}
}

func (c outboxPublisherCore[ID]) markFailed(ctx context.Context, mark outboxFailureMark[ID]) error {
	return c.repo.MarkAsFailed(ctx, mark.entryID, mark.claimToken, mark.errorMsg, mark.nextRetry)
}

func (c outboxPublisherCore[ID]) moveFailureToDLQ(ctx context.Context, mark outboxFailureMark[ID]) error {
	if c.dlq == nil || !mark.moveToDLQ {
		return nil
	}
	return c.dlq.MoveToDLQ(ctx, mark.dlqEntry)
}
