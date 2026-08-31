package eventsourced

import (
	"gochen/errors"
	"gochen/eventing"
	"gochen/messaging"
)

func defaultPayloadDecoder[T any]() func(eventing.IEvent) (T, error) {
	return func(evt eventing.IEvent) (T, error) {
		if evt == nil {
			var zero T
			return zero, errors.NewCode(errors.InvalidInput, "event cannot be nil")
		}
		payload, ok := messaging.PayloadAs[T](evt.GetPayload())
		if !ok {
			var zero T
			return zero, errors.NewCode(errors.InvalidInput, "payload type mismatch").
				WithContext("payload_type", evt.GetPayload().TypeName())
		}
		return payload, nil
	}
}
