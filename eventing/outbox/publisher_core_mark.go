package outbox

import (
	"context"
	"fmt"
	"time"

	"gochen/errors"
	"gochen/observe/logging"
)

type outboxFailureMark[ID comparable] struct {
	entry      OutboxEntry[ID]
	entryID    int64
	claimToken string
	errorMsg   string
	nextRetry  time.Time
	moveToDLQ  bool
	dlqEntry   OutboxEntry[ID]
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
		// MaxRetries <= 0（含 NoMaxRetries=-1）表示无重试上限，永远不进 DLQ；
		// 否则首次失败即会因 1 >= -1 恒成立而被误转移到死信队列。
		moveToDLQ: c.dlq != nil && c.cfg.MaxRetries > 0 && entry.RetryCount+1 >= c.cfg.MaxRetries,
		dlqEntry:  dlqEntry,
	}
}

func (c outboxPublisherCore[ID]) markFailed(ctx context.Context, mark outboxFailureMark[ID]) error {
	return c.repo.MarkAsFailed(ctx, mark.entryID, mark.claimToken, mark.errorMsg, mark.nextRetry)
}

// recoverPublishedMarkFailure 处理"事件已成功发布，但 MarkAsPublished 重试耗尽"的恢复。
//
// 该路径必须保持有界：把 mark 失败计入 RetryCount（MarkAsFailed），达到上限时走
// DLQ 逃生。若什么都不写（只留日志），条目会以 PROCESSING 滞留到租约到期被重新
// claim 并重复发布，RetryCount 永不增长、永不进入终态——MarkAsPublished 持续失败
// 时构成无界重投循环。为避免误导性的重复投递标记，errorMsg 显式注明事件已投递。
func (c outboxPublisherCore[ID]) recoverPublishedMarkFailure(ctx context.Context, entry OutboxEntry[ID], markPublishedErr error) error {
	if markPublishedErr == nil {
		return nil
	}
	mark := c.buildFailureMark(entry, fmt.Sprintf("event was published but mark as published failed: %v", markPublishedErr))
	parent := context.Background()
	if ctx != nil {
		parent = context.WithoutCancel(ctx)
	}
	recoverCtx, cancel := context.WithTimeout(parent, defaultParallelPublisherStopTimeout)
	defer cancel()
	if err := c.markFailureDirect(recoverCtx, mark); err != nil {
		return errors.Wrap(err, errors.Dependency, "recover published mark failure failed").
			WithContext("outbox_entry_id", entry.ID)
	}
	c.logger().Warn(ctx, "outbox published entry marked failed after mark failure; it will retry within bounded attempts and move to DLQ when exhausted",
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
