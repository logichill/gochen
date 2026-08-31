package outbox

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/messaging"
)

var _ IOutboxRepository[int64] = (*MemoryOutboxRepository[int64])(nil)

// MemoryOutboxRepository 提供基于纯内存的 Outbox 仓储实现。
type MemoryOutboxRepository[ID comparable] struct {
	mu          sync.RWMutex
	eventStore  *store.MemoryEventStore[ID]
	entries     []OutboxEntry[ID]
	eventIDs    map[string]struct{}
	nextID      int64
	nextClaimID uint64
	claimLease  time.Duration
}

// NewMemoryOutboxRepository 创建纯内存 Outbox 仓储。
//
// eventStore 必须与 DomainEventStore 使用同一个实例，避免事件写入后从另一份
// 内存存储恢复聚合。构造函数不创建隐藏的事件存储。
func NewMemoryOutboxRepository[ID comparable](eventStore *store.MemoryEventStore[ID]) (*MemoryOutboxRepository[ID], error) {
	if eventStore == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event store cannot be nil")
	}
	return &MemoryOutboxRepository[ID]{
		eventStore: eventStore,
		entries:    make([]OutboxEntry[ID], 0),
		eventIDs:   make(map[string]struct{}),
		nextID:     1,
		claimLease: defaultClaimLease,
	}, nil
}

func (m *MemoryOutboxRepository[ID]) SaveWithEvents(ctx context.Context, aggregateID ID, events []eventing.Event[ID]) error {
	if err := memoryOutboxContextError(ctx); err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	if m == nil {
		return errors.NewCode(errors.InvalidInput, "memory outbox repository is nil")
	}

	normalized, aggregateType, expectedVersion, err := normalizeMemoryOutboxEvents(aggregateID, events)
	if err != nil {
		return err
	}

	prepared := make([]OutboxEntry[ID], 0, len(normalized))
	for i := range normalized {
		entry, err := EventToOutboxEntry(aggregateID, normalized[i])
		if err != nil {
			return errors.Wrap(err, errors.InvalidInput, "serialize outbox event failed").
				WithContext("event_id", normalized[i].GetID())
		}
		prepared = append(prepared, *entry)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.initializeLocked()
	if isNilDependency(m.eventStore) {
		return errors.NewCode(errors.InvalidInput, "memory outbox event store is not configured")
	}

	missing := make([]OutboxEntry[ID], 0, len(prepared))
	batchEntries := make(map[string]OutboxEntry[ID], len(prepared))
	for i := range prepared {
		entry := prepared[i]
		if _, exists := m.eventIDs[entry.EventID]; exists {
			existing := m.entryByEventIDLocked(entry.EventID)
			if existing != nil && sameMemoryOutboxEvent(*existing, entry) {
				continue
			}
			return memoryOutboxDuplicateEvent(entry)
		}
		if existing, exists := batchEntries[entry.EventID]; exists {
			if sameMemoryOutboxEvent(existing, entry) {
				continue
			}
			return memoryOutboxDuplicateEvent(entry)
		}
		batchEntries[entry.EventID] = entry
		missing = append(missing, entry)
	}

	if err := m.eventStore.AppendEvents(ctx, aggregateType, aggregateID, store.ToStorable(normalized), expectedVersion); err != nil {
		return err
	}

	for i := range missing {
		missing[i].ID = m.nextID
		m.entries = append(m.entries, missing[i])
		m.eventIDs[missing[i].EventID] = struct{}{}
		m.nextID++
	}
	return nil
}

func normalizeMemoryOutboxEvents[ID comparable](aggregateID ID, events []eventing.Event[ID]) ([]eventing.Event[ID], string, uint64, error) {
	if len(events) == 0 {
		return nil, "", 0, nil
	}

	aggregateType := strings.TrimSpace(events[0].GetAggregateType())
	if aggregateType == "" {
		return nil, "", 0, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	firstVersion := events[0].GetVersion()
	if firstVersion == 0 {
		return nil, "", 0, errors.NewCode(errors.InvalidInput, "event version must be positive").
			WithContext("event_id", events[0].GetID()).
			WithContext("index", 0)
	}

	normalized := make([]eventing.Event[ID], 0, len(events))
	seen := make(map[string]eventing.Event[ID], len(events))
	for i := range events {
		event := events[i]
		if event.GetAggregateID() != aggregateID {
			return nil, "", 0, errors.NewCode(errors.InvalidInput, "event aggregate id does not match outbox aggregate id").
				WithContext("aggregate_id", aggregateID).
				WithContext("event_aggregate_id", event.GetAggregateID()).
				WithContext("event_id", event.GetID()).
				WithContext("index", i)
		}
		eventAggregateType := strings.TrimSpace(event.GetAggregateType())
		if eventAggregateType != aggregateType {
			return nil, "", 0, errors.NewCode(errors.InvalidInput, "event aggregate type does not match outbox aggregate type").
				WithContext("aggregate_type", aggregateType).
				WithContext("event_aggregate_type", event.GetAggregateType()).
				WithContext("event_id", event.GetID()).
				WithContext("index", i)
		}
		canonical := cloneMemoryOutboxEvent(event)
		canonical.SetAggregateType(eventAggregateType)
		if err := canonical.Validate(); err != nil {
			return nil, "", 0, err
		}
		if previous, exists := seen[event.GetID()]; exists {
			if !sameMemoryOutboxEventValue(previous, canonical) {
				return nil, "", 0, errors.NewCode(errors.Duplicate, "outbox event id already exists with different content").
					WithContext("event_id", event.GetID()).
					WithContext("index", i)
			}
			continue
		}
		expectedEventVersion := firstVersion + uint64(len(normalized))
		if event.GetVersion() != expectedEventVersion {
			return nil, "", 0, errors.NewCode(errors.InvalidInput, "event version not sequential").
				WithContext("event_id", event.GetID()).
				WithContext("expected_version", expectedEventVersion).
				WithContext("actual_version", event.GetVersion()).
				WithContext("index", i)
		}
		seen[event.GetID()] = canonical
		normalized = append(normalized, canonical)
	}

	return normalized, aggregateType, firstVersion - 1, nil
}

func cloneMemoryOutboxEvent[ID comparable](event eventing.Event[ID]) eventing.Event[ID] {
	clone := event
	if event.Metadata != nil {
		clone.Metadata = messaging.NewMetadata()
		for key, value := range event.Metadata.MapCopy() {
			clone.Metadata.Set(key, value)
		}
	}
	clone.Payload = messaging.NewPayload(messaging.CloneValue(messaging.PayloadValue(event.GetPayload())))
	return clone
}

func sameMemoryOutboxEventValue[ID comparable](left, right eventing.Event[ID]) bool {
	leftEntry, leftErr := EventToOutboxEntry(left.GetAggregateID(), left)
	rightEntry, rightErr := EventToOutboxEntry(right.GetAggregateID(), right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return sameMemoryOutboxEvent(*leftEntry, *rightEntry)
}

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
		if memoryOutboxEntryClaimable(entry, now) {
			entry.Status = OutboxStatusProcessing
			entry.ClaimToken = claimToken
			entry.LeaseUntil = &lease
			entry.NextRetryAt = nil
			claimed = append(claimed, cloneOutboxEntry(*entry))
		}
	}
	return claimed, nil
}

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

// GetClaimLease 返回内存仓储使用的 claim lease。
func (m *MemoryOutboxRepository[ID]) GetClaimLease() time.Duration {
	return m.effectiveClaimLease()
}

func (m *MemoryOutboxRepository[ID]) initializeLocked() {
	if m.eventIDs == nil {
		m.eventIDs = make(map[string]struct{})
		for i := range m.entries {
			m.eventIDs[m.entries[i].EventID] = struct{}{}
		}
	}
	if m.nextID <= 0 {
		m.nextID = 1
		for i := range m.entries {
			if m.entries[i].ID >= m.nextID {
				m.nextID = m.entries[i].ID + 1
			}
		}
	}
}

func (m *MemoryOutboxRepository[ID]) effectiveClaimLease() time.Duration {
	if m == nil || m.claimLease <= 0 {
		return defaultClaimLease
	}
	return m.claimLease
}

func (m *MemoryOutboxRepository[ID]) entryByEventIDLocked(eventID string) *OutboxEntry[ID] {
	for i := range m.entries {
		if m.entries[i].EventID == eventID {
			return &m.entries[i]
		}
	}
	return nil
}

func memoryOutboxContextError(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx.Err()
}

func memoryOutboxEntryClaimable[ID comparable](entry *OutboxEntry[ID], now time.Time) bool {
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

func memoryOutboxDuplicateEvent[ID comparable](entry OutboxEntry[ID]) error {
	return errors.NewCode(errors.Duplicate, "outbox event id already exists with different content").
		WithContext("event_id", entry.EventID).
		WithContext("event_type", entry.EventType)
}

func sameMemoryOutboxEvent[ID comparable](left, right OutboxEntry[ID]) bool {
	return left.AggregateID == right.AggregateID &&
		left.AggregateType == right.AggregateType &&
		left.EventType == right.EventType &&
		left.EventData == right.EventData
}

func cloneOutboxEntry[ID comparable](entry OutboxEntry[ID]) OutboxEntry[ID] {
	if entry.PublishedAt != nil {
		value := *entry.PublishedAt
		entry.PublishedAt = &value
	}
	if entry.LeaseUntil != nil {
		value := *entry.LeaseUntil
		entry.LeaseUntil = &value
	}
	if entry.NextRetryAt != nil {
		value := *entry.NextRetryAt
		entry.NextRetryAt = &value
	}
	return entry
}
