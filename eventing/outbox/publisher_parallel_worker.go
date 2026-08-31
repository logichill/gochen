package outbox

import (
	"context"

	"gochen/observe/logging"
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

func (p *ParallelPublisher[ID]) processEntry(ctx context.Context, entry OutboxEntry[ID]) {
	core := p.core()
	strategy := outboxMarkStrategy[ID]{
		markPublished: func(ctx context.Context, entry OutboxEntry[ID]) error {
			if p.tryEnqueueMark(ctx, markOp[ID]{kind: markPublished, entryID: entry.ID, claimToken: entry.ClaimToken, eventID: entry.EventID}) {
				return nil
			}
			return core.markPublishedWithRecovery(ctx, entry)
		},
		markFailed: p.markFailure,
	}
	if err := core.processClaimed(ctx, entry, strategy); err != nil {
		p.log.Error(ctx, "outbox entry processing failed",
			logging.Int64("entry_id", entry.ID),
			logging.Error(err))
	}
}

// markFailure 通过批量回写策略标记记录为失败，队列不可用时退回单条直写。
func (p *ParallelPublisher[ID]) markFailure(ctx context.Context, mark outboxFailureMark[ID]) error {
	core := p.core()

	if handled, err := core.markFailedAndMoveToDLQ(ctx, mark); handled {
		if err == nil {
			return nil
		}
		p.log.Warn(ctx, "atomic DLQ move failed; falling back to non-atomic recovery",
			logging.Int64("entry_id", mark.entryID),
			logging.Error(err))
	}

	op := markOp[ID]{
		kind:       markFailed,
		entryID:    mark.entryID,
		claimToken: mark.claimToken,
		errorMsg:   mark.errorMsg,
		nextRetry:  mark.nextRetry,
		moveToDLQ:  mark.moveToDLQ,
		entry:      mark.entry,
		dlqEntry:   mark.dlqEntry,
	}
	if p.tryEnqueueMark(ctx, op) {
		return nil
	}

	if err := core.markFailed(ctx, mark); err != nil {
		return err
	}
	return core.moveFailureToDLQ(ctx, mark)
}
