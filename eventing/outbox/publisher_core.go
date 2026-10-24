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
