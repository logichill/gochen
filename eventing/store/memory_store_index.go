package store

import (
	"fmt"
	"sort"

	"gochen/errors"
	"gochen/eventing"
)

func (m *MemoryEventStore[ID]) initializeUnsafe() {
	if m.events == nil {
		m.events = make(map[memoryAggregateKey[ID]][]eventing.Event[ID])
	}
	if m.eventByID == nil {
		m.eventByID = make(map[string]eventing.Event[ID])
	}
}

type memoryAggregateKey[ID comparable] struct {
	aggregateType string
	aggregateID   ID
}

// getAggregateVersionUnsafe 在持锁前提下读取某个复合键对应的最新版本号。
func (m *MemoryEventStore[ID]) getAggregateVersionUnsafe(key memoryAggregateKey[ID]) (uint64, error) {
	aggregateEvents, exists := m.events[key]
	if !exists || len(aggregateEvents) == 0 {
		return 0, nil
	}
	return aggregateEvents[len(aggregateEvents)-1].GetVersion(), nil
}

func (m *MemoryEventStore[ID]) validateEventIDUniquenessUnsafe(aggregateID ID, events []eventing.Event[ID]) error {
	seen := make(map[string]eventing.Event[ID], len(events))
	for _, evt := range events {
		if _, ok := m.eventByID[evt.GetID()]; ok {
			return duplicateMemoryEventIDError(aggregateID, evt)
		}
		if _, ok := seen[evt.GetID()]; ok {
			return duplicateMemoryEventIDError(aggregateID, evt)
		}
		seen[evt.GetID()] = evt
	}
	return nil
}

func duplicateMemoryEventIDError[ID comparable](aggregateID ID, evt eventing.Event[ID]) error {
	return errors.NewCode(errors.Duplicate, fmt.Sprintf("event %s already exists", evt.GetID())).
		WithContext("event_id", evt.GetID()).
		WithContext("event_type", evt.GetType()).
		WithContext("aggregate_id", aggregateID).
		WithContext("version", evt.GetVersion())
}

func (m *MemoryEventStore[ID]) insertGlobalEventsUnsafe(events []eventing.Event[ID]) {
	if len(events) == 0 {
		return
	}
	incoming := append([]eventing.Event[ID](nil), events...)
	sort.Slice(incoming, func(i, j int) bool {
		return eventBefore(incoming[i], incoming[j])
	})
	if len(m.globalEvents) == 0 || !eventBefore(incoming[0], m.globalEvents[len(m.globalEvents)-1]) {
		m.globalEvents = append(m.globalEvents, incoming...)
		return
	}

	merged := make([]eventing.Event[ID], 0, len(m.globalEvents)+len(incoming))
	i, j := 0, 0
	for i < len(m.globalEvents) && j < len(incoming) {
		if eventBefore(incoming[j], m.globalEvents[i]) {
			merged = append(merged, incoming[j])
			j++
		} else {
			merged = append(merged, m.globalEvents[i])
			i++
		}
	}
	merged = append(merged, m.globalEvents[i:]...)
	merged = append(merged, incoming[j:]...)
	m.globalEvents = merged
}
