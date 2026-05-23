package eventsourced

import (
	"context"
	"fmt"

	"gochen/domain"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/messaging"
)

func asDomainEvent[ID comparable](
	ctx context.Context,
	reg *registry.Registry,
	upgraders *upcast.UpgraderRegistry,
	evt *eventing.Event[ID],
) (domain.IDomainEvent, error) {
	if evt == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event cannot be nil")
	}
	if _, err := upcast.UpgradeEventPayload(ctx, reg, upgraders, evt); err != nil {
		return nil, err
	}
	payload := messaging.PayloadValue(evt.GetPayload())
	domainEvt, ok := payload.(domain.IDomainEvent)
	if !ok {
		return nil, errors.NewCode(errors.Internal, "event payload does not implement domain.IDomainEvent").
			WithContext("payload_type", fmtType(payload)).
			WithContext("event_type", evt.GetType()).
			WithContext("event_id", evt.GetID())
	}
	return domainEvt, nil
}

func fmtType(v any) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T", v)
}
