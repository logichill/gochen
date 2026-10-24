package store

import (
	"bytes"
	"encoding/json"

	"gochen/eventing"
	"gochen/messaging"
)

func sameMemoryEvent[ID comparable](a eventing.Event[ID], b eventing.Event[ID]) bool {
	return a.GetID() == b.GetID() &&
		a.GetKind() == b.GetKind() &&
		a.GetType() == b.GetType() &&
		a.GetTimestamp().Equal(b.GetTimestamp()) &&
		a.GetAggregateID() == b.GetAggregateID() &&
		a.GetAggregateType() == b.GetAggregateType() &&
		a.GetVersion() == b.GetVersion() &&
		a.EventSchemaVersion() == b.EventSchemaVersion() &&
		sameMemoryPayload(a.GetPayload(), b.GetPayload()) &&
		sameMemoryMetadata(a.GetMetadata(), b.GetMetadata())
}

func sameMemoryPayload(a messaging.Payload, b messaging.Payload) bool {
	left, leftOK := memoryPayloadBytes(a)
	right, rightOK := memoryPayloadBytes(b)
	return leftOK && rightOK && bytes.Equal(left, right)
}

func memoryPayloadBytes(payload messaging.Payload) ([]byte, bool) {
	data, err := json.Marshal(messaging.PayloadValue(payload))
	if err != nil {
		return nil, false
	}
	return data, true
}

func sameMemoryMetadata(a *messaging.Metadata, b *messaging.Metadata) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	left := a.MapCopy()
	right := b.MapCopy()
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		if rightValue, ok := right[key]; !ok || rightValue != leftValue {
			return false
		}
	}
	return true
}

func cloneMemoryEvents[ID comparable](events []eventing.Event[ID]) []eventing.Event[ID] {
	if len(events) == 0 {
		return []eventing.Event[ID]{}
	}
	out := make([]eventing.Event[ID], len(events))
	for i := range events {
		out[i] = cloneMemoryEvent(events[i])
	}
	return out
}

func cloneMemoryEvent[ID comparable](evt eventing.Event[ID]) eventing.Event[ID] {
	clone := evt
	clone.Payload = messaging.NewPayload(messaging.CloneValue(messaging.PayloadValue(evt.GetPayload())))
	if evt.Metadata != nil {
		clone.Metadata = messaging.NewMetadata()
		for k, v := range evt.Metadata.MapCopy() {
			clone.Metadata.Set(k, v)
		}
	}
	return clone
}
