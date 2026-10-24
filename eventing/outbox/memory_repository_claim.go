package outbox

import (
	"context"
	"fmt"
	"time"

	"gochen/errors"
)

func (m *MemoryOutboxRepository[ID]) ClaimPendingEntries(ctx context.Context, limit int) ([]OutboxEntry[ID], error) {
	if err := memoryOutboxContextError(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, nil
	}
	if m == nil {
		return nil, errors.NewCode(errors.InvalidInput, "memory outbox repository is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.initializeLocked()

	claimed := make([]OutboxEntry[ID], 0, min(limit, len(m.entries)))
	now := time.Now()
	lease := now.Add(m.effectiveClaimLease())
	m.nextClaimID++
	claimToken := fmt.Sprintf("memory-claim-%d", m.nextClaimID)

	for i := range m.entries {
		if len(claimed) >= limit {
			break
		}
		entry := &m.entries[i]
		if memoryOutboxEntryClaimable(entry, now, m.maxRetries) {
			entry.Status = OutboxStatusProcessing
			entry.ClaimToken = claimToken
			entry.LeaseUntil = &lease
			entry.NextRetryAt = nil
			claimed = append(claimed, cloneOutboxEntry(*entry))
		}
	}
	return claimed, nil
}

// RenewClaim 延长当前 worker 持有的 claim lease。
func (m *MemoryOutboxRepository[ID]) RenewClaim(ctx context.Context, entryID int64, claimToken string) error {
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
		if m.entries[i].ID != entryID {
			continue
		}
		if !memoryOutboxClaimOwned(&m.entries[i], claimToken) {
			return memoryOutboxClaimConflict(entryID)
		}
		leaseUntil := time.Now().Add(m.effectiveClaimLease())
		m.entries[i].LeaseUntil = &leaseUntil
		return nil
	}
	return memoryOutboxClaimConflict(entryID)
}

// GetClaimLease 返回内存仓储使用的 claim lease。
func (m *MemoryOutboxRepository[ID]) GetClaimLease() time.Duration {
	return m.effectiveClaimLease()
}

func (m *MemoryOutboxRepository[ID]) effectiveClaimLease() time.Duration {
	if m == nil || m.claimLease <= 0 {
		return defaultClaimLease
	}
	return m.claimLease
}

func memoryOutboxEntryClaimable[ID comparable](entry *OutboxEntry[ID], now time.Time, maxRetries int) bool {
	if entry == nil {
		return false
	}
	// maxRetries 门禁只拦截 Pending 超限条目；Failed 与租约到期的 Processing 条目
	// 即使 RetryCount 已达上限也保持可领取。这是 DLQ 迁移失败或未配置 DLQ 时唯一
	// 的重试迁移逃生路径：超限条目一旦不可领取，markFailureDirect 中"markFailed
	// 成功但 moveFailureToDLQ 失败"的条目将永久冻结在 outbox（既不在可重试管道内、
	// 也不在 DLQ 内）。SQL 仓储的超限回收依赖 dead_lettered 终态与 cleanup；内存
	// 仓储没有这些机制，必须保留重试出口（与 HEAD 的无限重试语义一致）。
	if maxRetries > 0 && entry.RetryCount >= maxRetries && entry.Status == OutboxStatusPending {
		return false
	}
	switch entry.Status {
	case OutboxStatusPending:
		return true
	case OutboxStatusFailed:
		return entry.NextRetryAt == nil || !entry.NextRetryAt.After(now)
	case OutboxStatusProcessing:
		return entry.LeaseUntil != nil && !entry.LeaseUntil.After(now)
	default:
		return false
	}
}

func memoryOutboxClaimOwned[ID comparable](entry *OutboxEntry[ID], claimToken string) bool {
	return entry.Status == OutboxStatusProcessing && entry.ClaimToken == claimToken
}

func memoryOutboxClaimConflict(entryID int64) error {
	return errors.NewCode(errors.Conflict, "outbox entry claim is no longer owned").WithContext("entry_id", entryID)
}
