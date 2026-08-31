package outbox

import (
	"context"
	"time"

	"gochen/observe/logging"
)

type markKind int

const (
	markPublished markKind = iota
	markFailed
)

type markOp[ID comparable] struct {
	kind       markKind
	entryID    int64
	claimToken string
	eventID    string
	errorMsg   string
	nextRetry  time.Time
	moveToDLQ  bool
	// entry 是未自增 RetryCount 的原始记录，供原子 DLQ mover 内部自增；
	// dlqEntry 是已自增 RetryCount 的目标记录，供非原子回退路径直接写入。二者不可混用。
	entry    OutboxEntry[ID]
	dlqEntry OutboxEntry[ID]
}

func (p *ParallelPublisher[ID]) markLoop(ctx context.Context) {
	defer close(p.markDoneCh)

	const flushInterval = 200 * time.Millisecond
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	maxBatch := p.cfg.BatchSize
	if maxBatch <= 0 {
		maxBatch = 100
	}
	// 防止生成过长的 IN (...) 语句导致性能退化或超出数据库限制
	if maxBatch > 500 {
		maxBatch = 500
	}

	publishedIDs := make([]markOp[ID], 0, maxBatch)
	failedOps := make([]markOp[ID], 0, maxBatch)

	collect := func(op markOp[ID]) {
		switch op.kind {
		case markPublished:
			publishedIDs = append(publishedIDs, op)
		case markFailed:
			failedOps = append(failedOps, op)
		}
	}

	flush := func(final bool) {
		if p.batchOps == nil {
			publishedIDs = publishedIDs[:0]
			failedOps = failedOps[:0]
			return
		}
		flushCtx, cancel := p.markFlushContext(ctx, final)
		defer cancel()

		if len(publishedIDs) > 0 {
			p.flushPublishedOps(ctx, flushCtx, publishedIDs)
		}

		if len(failedOps) > 0 {
			p.flushFailedOps(ctx, flushCtx, failedOps)
		}

		publishedIDs = publishedIDs[:0]
		failedOps = failedOps[:0]
	}

	drainReady := func() bool {
		for {
			select {
			case op, ok := <-p.markCh:
				if !ok {
					return true
				}
				collect(op)
			default:
				return false
			}
		}
	}

	ctxDone := ctx.Done()
	for {
		select {
		case op, ok := <-p.markCh:
			if !ok {
				flush(true)
				return
			}
			collect(op)
			if len(publishedIDs)+len(failedOps) >= maxBatch {
				flush(false)
			}
		case <-ticker.C:
			flush(false)
		case <-p.markStopCh:
			drainReady()
			flush(true)
			return
		case <-ctxDone:
			closed := drainReady()
			flush(true)
			if closed {
				return
			}
			ctxDone = nil
		}
	}
}

func (p *ParallelPublisher[ID]) flushPublishedOps(ctx context.Context, flushCtx context.Context, publishedOps []markOp[ID]) {
	// batchOps may be implemented by downstream repositories; keep flush input isolated.
	ops := append([]markOp[ID](nil), publishedOps...)
	entries := make([]ClaimedEntry, 0, len(ops))
	for _, op := range ops {
		entries = append(entries, ClaimedEntry{ID: op.entryID, ClaimToken: op.claimToken})
	}
	if err := p.batchOps.MarkAsPublishedBatch(flushCtx, entries); err != nil {
		p.log.Warn(ctx, "batch mark as published failed, falling back to per-entry updates",
			logging.Int("count", len(entries)),
			logging.Error(err))
		recoveryCtx, cancel := detachedMarkRecoveryContext(flushCtx)
		defer cancel()
		for i, entry := range entries {
			op := ops[i]
			if markErr := p.core().markPublishedAfterSuccessfulPublishWithinContext(recoveryCtx, entry, p.cfg.MarkPublishedAttempts); markErr != nil {
				p.log.Error(ctx, "recover published entry after mark failure",
					logging.Int64("entry_id", entry.ID),
					logging.Error(markErr))
				recoveryEntry := OutboxEntry[ID]{ID: op.entryID, EventID: op.eventID, ClaimToken: op.claimToken}
				if recoverErr := p.core().recoverPublishedMarkFailure(recoveryCtx, recoveryEntry, markErr); recoverErr != nil {
					p.log.Error(ctx, "recover published entry failed",
						logging.Int64("entry_id", entry.ID),
						logging.Error(recoverErr))
				}
			}
		}
	}
}

func (p *ParallelPublisher[ID]) flushFailedOps(ctx context.Context, flushCtx context.Context, failedOps []markOp[ID]) {
	// batchOps may be implemented by downstream repositories; keep flush input isolated.
	ops := append([]markOp[ID](nil), failedOps...)
	core := p.core()
	var recoveryCtx context.Context
	var recoveryCancel context.CancelFunc
	getRecoveryContext := func() context.Context {
		if recoveryCtx == nil {
			recoveryCtx, recoveryCancel = detachedMarkRecoveryContext(flushCtx)
		}
		return recoveryCtx
	}
	defer func() {
		if recoveryCancel != nil {
			recoveryCancel()
		}
	}()
	entries := make([]FailedEntry, 0, len(ops))
	nonAtomicOps := make([]markOp[ID], 0, len(ops))
	for _, op := range ops {
		mark := op.failureMark()
		operationCtx := flushCtx
		if operationCtx == nil || operationCtx.Err() != nil {
			operationCtx = getRecoveryContext()
		}
		if handled, err := core.markFailedAndMoveToDLQ(operationCtx, mark); handled {
			if err == nil {
				continue
			}
			p.log.Warn(ctx, "atomic DLQ move failed; falling back to non-atomic recovery",
				logging.Int64("entry_id", op.entryID),
				logging.Error(err))
			if p.recoverAtomicDLQFallback(ctx, getRecoveryContext(), core, mark) == nil {
				continue
			}
		}
		nonAtomicOps = append(nonAtomicOps, op)
		entries = append(entries, FailedEntry{
			ClaimedEntry: ClaimedEntry{ID: op.entryID, ClaimToken: op.claimToken},
			Error:        op.errorMsg,
			NextRetryAt:  op.nextRetry,
		})
	}
	if len(entries) == 0 {
		return
	}
	batchCtx := flushCtx
	if batchCtx == nil || batchCtx.Err() != nil {
		batchCtx = getRecoveryContext()
	}
	if err := p.batchOps.MarkAsFailedBatch(batchCtx, entries); err != nil {
		p.log.Warn(ctx, "batch mark as failed failed, falling back to per-entry updates",
			logging.Int("count", len(entries)),
			logging.Error(err))
		fallbackCtx := getRecoveryContext()
		for _, op := range nonAtomicOps {
			mark := op.failureMark()
			if err := core.markFailed(fallbackCtx, mark); err != nil {
				p.log.Error(ctx, "mark as failed failed",
					logging.Int64("entry_id", op.entryID),
					logging.Error(err))
				continue
			}
			if err := core.moveFailureToDLQ(fallbackCtx, mark); err != nil {
				p.log.Error(ctx, "move to DLQ failed",
					logging.Int64("entry_id", op.entryID),
					logging.Error(err))
			}
		}
		return
	}
	for _, op := range nonAtomicOps {
		mark := op.failureMark()
		moveCtx := flushCtx
		if moveCtx == nil || moveCtx.Err() != nil {
			moveCtx = getRecoveryContext()
		}
		if err := core.moveFailureToDLQ(moveCtx, mark); err != nil {
			p.log.Error(ctx, "move to DLQ failed",
				logging.Int64("entry_id", op.entryID),
				logging.Error(err))
		}
	}
}

func detachedMarkRecoveryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	parent := context.Background()
	if ctx != nil {
		parent = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(parent, defaultParallelPublisherStopTimeout)
}

func (op markOp[ID]) failureMark() outboxFailureMark[ID] {
	return outboxFailureMark[ID]{
		entry:      op.entry,
		entryID:    op.entryID,
		claimToken: op.claimToken,
		errorMsg:   op.errorMsg,
		nextRetry:  op.nextRetry,
		moveToDLQ:  op.moveToDLQ,
		dlqEntry:   op.dlqEntry,
	}
}

func (p *ParallelPublisher[ID]) recoverAtomicDLQFallback(ctx context.Context, flushCtx context.Context, core outboxPublisherCore[ID], mark outboxFailureMark[ID]) error {
	if err := core.markFailed(flushCtx, mark); err != nil {
		p.log.Warn(ctx, "non-atomic DLQ fallback mark failed; entry may already be moved to DLQ",
			logging.Int64("entry_id", mark.entryID),
			logging.Error(err))
		return err
	}
	if err := core.moveFailureToDLQ(flushCtx, mark); err != nil {
		p.log.Error(ctx, "move to DLQ failed",
			logging.Int64("entry_id", mark.entryID),
			logging.Error(err))
		return err
	}
	return nil
}

func (p *ParallelPublisher[ID]) markFlushContext(ctx context.Context, final bool) (context.Context, context.CancelFunc) {
	if !final && ctx != nil && ctx.Err() == nil {
		return ctx, func() {}
	}
	if final {
		p.mu.Lock()
		stopCtx := p.markStopCtx
		p.mu.Unlock()
		if stopCtx != nil && stopCtx.Err() == nil {
			return context.WithTimeout(stopCtx, defaultParallelPublisherStopTimeout)
		}
	}
	return context.WithTimeout(context.Background(), defaultParallelPublisherStopTimeout)
}
