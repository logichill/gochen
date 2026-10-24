package outbox

import (
	"context"
	"time"

	"gochen/errors"
)

func (m *MemoryOutboxRepository[ID]) MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error {
	if err := memoryOutboxContextError(ctx); err != nil {
		return err
	}
	if m == nil {
		return errors.NewCode(errors.InvalidInput, "memory outbox repository is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	for i := range m.entries {
		if m.entries[i].ID == entryID {
			if !memoryOutboxClaimOwned(&m.entries[i], claimToken) {
				return memoryOutboxClaimConflict(entryID)
			}
			now := time.Now()
			m.entries[i].Status = OutboxStatusPublished
			m.entries[i].ClaimToken = ""
			m.entries[i].LeaseUntil = nil
			m.entries[i].PublishedAt = &now
			m.entries[i].NextRetryAt = nil
			return nil
		}
	}
	return memoryOutboxClaimConflict(entryID)
}

func (m *MemoryOutboxRepository[ID]) MarkAsFailed(ctx context.Context, entryID int64, claimToken string, errReason string, nextRetry time.Time) error {
	if err := memoryOutboxContextError(ctx); err != nil {
		return err
	}
	if m == nil {
		return errors.NewCode(errors.InvalidInput, "memory outbox repository is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	for i := range m.entries {
		if m.entries[i].ID == entryID {
			if !memoryOutboxClaimOwned(&m.entries[i], claimToken) {
				return memoryOutboxClaimConflict(entryID)
			}
			m.entries[i].Status = OutboxStatusFailed
			m.entries[i].ClaimToken = ""
			m.entries[i].LeaseUntil = nil
			m.entries[i].LastError = errReason
			m.entries[i].RetryCount++
			m.entries[i].NextRetryAt = &nextRetry
			return nil
		}
	}
	return memoryOutboxClaimConflict(entryID)
}

// DeletePublished 删除发布时间早于 olderThan 的已发布记录。
func (m *MemoryOutboxRepository[ID]) DeletePublished(ctx context.Context, olderThan time.Time) error {
	if err := memoryOutboxContextError(ctx); err != nil {
		return err
	}
	if m == nil {
		return errors.NewCode(errors.InvalidInput, "memory outbox repository is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	entries := m.entries
	kept := entries[:0]
	for i := range entries {
		entry := entries[i]
		if entry.Status == OutboxStatusPublished && entry.PublishedAt != nil && entry.PublishedAt.Before(olderThan) {
			delete(m.eventIDs, entry.EventID)
			continue
		}
		kept = append(kept, entry)
	}
	clear(entries[len(kept):])
	m.entries = kept
	return nil
}
