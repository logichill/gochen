package proj

import (
	"context"

	"gochen/domain/eventsourced"
	"gochen/eventing"
	"gochen/eventing/projection"
)

// OrderCreated 事件载荷
type OrderCreated struct{ Amount int }

var total int

func GetTotal() int { return total }

func NewOrderAmountProjection() (projection.IProjection, error) {
	return eventsourced.NewEventSourcedProjection[*OrderCreated](eventsourced.EventSourcedProjectionOption[*OrderCreated]{
		Name:       "order_amount_sql_checkpoint",
		EventTypes: []string{"OrderCreated"},
		Decoder: func(evt eventing.Event) (*OrderCreated, error) {
			if p, ok := evt.Payload.(*OrderCreated); ok {
				return p, nil
			}
			return nil, nil
		},
		Handle: func(ctx context.Context, payload *OrderCreated) error {
			if payload == nil {
				return nil
			}
			total += payload.Amount
			return nil
		},
	})
}
