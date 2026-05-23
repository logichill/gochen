package outbox

import (
	"context"
	"time"

	"gochen/logging"
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
	errorMsg   string
	nextRetry  time.Time
	moveToDLQ  bool
	dlqEntry   OutboxEntry[ID]
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

	flush := func(final bool) {
		if p.batchOps == nil {
			publishedIDs = publishedIDs[:0]
			failedOps = failedOps[:0]
			return
		}
		flushCtx, cancel := markFlushContext(ctx, final)
		defer cancel()

		if len(publishedIDs) > 0 {
			ops := append([]markOp[ID](nil), publishedIDs...)
			entries := make([]ClaimedEntry, 0, len(ops))
			for _, op := range ops {
				entries = append(entries, ClaimedEntry{ID: op.entryID, ClaimToken: op.claimToken})
			}
			if err := p.batchOps.MarkAsPublishedBatch(flushCtx, entries); err != nil {
				p.log.Warn(ctx, "batch mark as published failed, falling back to per-entry updates",
					logging.Int("count", len(entries)),
					logging.Error(err))
				for _, entry := range entries {
					_ = p.markPublishedAfterSuccessfulPublish(flushCtx, entry)
				}
			}
		}

		if len(failedOps) > 0 {
			ops := append([]markOp[ID](nil), failedOps...)
			entries := make([]FailedEntry, 0, len(ops))
			for _, op := range ops {
				entries = append(entries, FailedEntry{
					ClaimedEntry: ClaimedEntry{ID: op.entryID, ClaimToken: op.claimToken},
					Error:        op.errorMsg,
					NextRetryAt:  op.nextRetry,
				})
			}
			if err := p.batchOps.MarkAsFailedBatch(flushCtx, entries); err != nil {
				p.log.Warn(ctx, "batch mark as failed failed, falling back to per-entry updates",
					logging.Int("count", len(entries)),
					logging.Error(err))
				core := p.core()
				for _, op := range ops {
					mark := outboxFailureMark[ID]{
						entryID:    op.entryID,
						claimToken: op.claimToken,
						errorMsg:   op.errorMsg,
						nextRetry:  op.nextRetry,
						moveToDLQ:  op.moveToDLQ,
						dlqEntry:   op.dlqEntry,
					}
					if err := core.markFailed(flushCtx, mark); err != nil {
						p.log.Error(ctx, "mark as failed failed",
							logging.Int64("entry_id", op.entryID),
							logging.Error(err))
						continue
					}
					if err := core.moveFailureToDLQ(flushCtx, mark); err != nil {
						p.log.Error(ctx, "move to DLQ failed",
							logging.Int64("entry_id", op.entryID),
							logging.Error(err))
					}
				}
			} else {
				// DLQ：仅在 DB 标记失败成功后移动
				core := p.core()
				for _, op := range ops {
					mark := outboxFailureMark[ID]{
						entryID:    op.entryID,
						claimToken: op.claimToken,
						errorMsg:   op.errorMsg,
						nextRetry:  op.nextRetry,
						moveToDLQ:  op.moveToDLQ,
						dlqEntry:   op.dlqEntry,
					}
					if err := core.moveFailureToDLQ(flushCtx, mark); err != nil {
						p.log.Error(ctx, "move to DLQ failed",
							logging.Int64("entry_id", op.entryID),
							logging.Error(err))
					}
				}
			}
		}

		publishedIDs = publishedIDs[:0]
		failedOps = failedOps[:0]
	}

	for {
		select {
		case op, ok := <-p.markCh:
			if !ok {
				flush(true)
				return
			}
			switch op.kind {
			case markPublished:
				publishedIDs = append(publishedIDs, op)
			case markFailed:
				failedOps = append(failedOps, op)
			}
			if len(publishedIDs)+len(failedOps) >= maxBatch {
				flush(false)
			}
		case <-ticker.C:
			flush(false)
		}
	}
}

func markFlushContext(ctx context.Context, final bool) (context.Context, context.CancelFunc) {
	if !final && ctx != nil && ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.Background(), defaultParallelPublisherStopTimeout)
}
