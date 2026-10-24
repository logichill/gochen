package outbox

import (
	"context"
	"strings"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/messaging"
)

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

func (m *MemoryOutboxRepository[ID]) entryByEventIDLocked(eventID string) *OutboxEntry[ID] {
	for i := range m.entries {
		if m.entries[i].EventID == eventID {
			return &m.entries[i]
		}
	}
	return nil
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
