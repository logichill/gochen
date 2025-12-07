package proj

import (
	"context"
	"sync"

	"gochen/app/eventsourced"
	"gochen/eventing"
	"gochen/eventing/projection"
)

// OrderCreated 事件载荷
type OrderCreated struct{ Amount int }

// 内存读模型：累计金额
var (
	totalAmount int
	mu          sync.RWMutex
)

// GetTotal 返回累计金额（供示例读取）
func GetTotal() int { mu.RLock(); defer mu.RUnlock(); return totalAmount }

// NewOrderAmountProjection 构建投影：订阅 OrderCreated，累计金额到内存读模型
func NewOrderAmountProjection() (projection.IProjection, error) {
	return eventsourced.NewEventSourcedProjection[*OrderCreated](eventsourced.EventSourcedProjectionOption[*OrderCreated]{
		Name:       "order_amount_projection",
		EventTypes: []string{"OrderCreated"},
		Decoder: func(evt eventing.IEvent) (*OrderCreated, error) {
			if evt == nil {
				return nil, nil
			}
			if p, ok := evt.GetPayload().(*OrderCreated); ok {
				return p, nil
			}
			return nil, nil
		},
		Handle: func(ctx context.Context, payload *OrderCreated) error {
			if payload == nil {
				return nil
			}
			mu.Lock()
			totalAmount += payload.Amount
			mu.Unlock()
			return nil
		},
	})
}
