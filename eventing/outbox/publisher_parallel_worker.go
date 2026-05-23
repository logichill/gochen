package outbox

import (
	"context"

	"gochen/errors"
	"gochen/logging"
)

func (p *ParallelPublisher[ID]) worker(ctx context.Context, id int, ch <-chan OutboxEntry[ID]) {
	defer p.wg.Done()

	p.log.Debug(ctx, "worker started", logging.Int("worker_id", id))

	for entry := range ch {
		processCtx, cancel := claimedEntryContext(ctx)
		p.processEntry(processCtx, entry)
		cancel()
	}
	p.log.Debug(ctx, "worker stopped", logging.Int("worker_id", id))
}

func claimedEntryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx != nil && ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.Background(), defaultParallelPublisherStopTimeout)
}

func (p *ParallelPublisher[ID]) markPublishedAfterSuccessfulPublish(ctx context.Context, entry ClaimedEntry) bool {
	return p.core().markPublishedAfterSuccessfulPublish(ctx, entry, 3)
}

func (p *ParallelPublisher[ID]) processEntry(ctx context.Context, entry OutboxEntry[ID]) {
	log := p.log
	if log == nil {
		log = logging.NewNoopLogger()
	}
	if isNilDependency(p.repo) {
		log.Error(ctx, "outbox repository is nil", logging.Int64("entry_id", entry.ID))
		return
	}
	if isNilDependency(p.bus) {
		err := errors.NewCode(errors.InvalidInput, "event bus cannot be nil")
		log.Error(ctx, "event bus is nil", logging.Int64("entry_id", entry.ID), logging.Error(err))
		p.markFailed(ctx, entry, err.Error())
		return
	}

	result := p.core().publishClaimed(ctx, entry)
	if result.keepaliveErr != nil {
		p.log.Error(ctx, "outbox claim keepalive failed",
			logging.Int64("entry_id", entry.ID),
			logging.String("event_type", entry.EventType),
			logging.Error(result.keepaliveErr))
		return
	}
	if result.err != nil {
		p.log.Warn(ctx, result.stage.warnLogMessage(),
			logging.Int64("entry_id", entry.ID),
			logging.String("event_type", entry.EventType),
			logging.Error(result.err))
		p.markFailed(ctx, entry, result.err.Error())
		return
	}

	// 标记为已发布
	if p.batchOps != nil && p.markCh != nil {
		select {
		case p.markCh <- markOp[ID]{kind: markPublished, entryID: entry.ID, claimToken: entry.ClaimToken}:
		default:
			// 队列满时回退到逐条更新，避免阻塞发布路径
			if !p.markPublishedAfterSuccessfulPublish(ctx, ClaimedEntry{ID: entry.ID, ClaimToken: entry.ClaimToken}) {
				return
			}
		}
	} else {
		if !p.markPublishedAfterSuccessfulPublish(ctx, ClaimedEntry{ID: entry.ID, ClaimToken: entry.ClaimToken}) {
			return
		}
	}

	p.log.Debug(ctx, "event published successfully",
		logging.Int64("entry_id", entry.ID),
		logging.String("event_type", entry.EventType))
}

// markFailed 标记记录为失败。
func (p *ParallelPublisher[ID]) markFailed(ctx context.Context, entry OutboxEntry[ID], errorMsg string) {
	core := p.core()
	mark := core.buildFailureMark(entry, errorMsg)

	if p.batchOps != nil && p.markCh != nil {
		op := markOp[ID]{
			kind:       markFailed,
			entryID:    mark.entryID,
			claimToken: mark.claimToken,
			errorMsg:   errorMsg,
			nextRetry:  mark.nextRetry,
			moveToDLQ:  mark.moveToDLQ,
			dlqEntry:   mark.dlqEntry,
		}
		select {
		case p.markCh <- op:
			return
		default:
			// 队列满时回退到逐条更新，避免阻塞发布路径
		}
	}

	if err := core.markFailed(ctx, mark); err != nil {
		p.log.Error(ctx, "mark as failed failed",
			logging.Int64("entry_id", entry.ID),
			logging.Error(err))
		return
	}

	if err := core.moveFailureToDLQ(ctx, mark); err != nil {
		p.log.Error(ctx, "move to DLQ failed",
			logging.Int64("entry_id", entry.ID),
			logging.Error(err))
	}
}
