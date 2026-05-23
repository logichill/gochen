package proj

import (
	"context"

	"gochen/app/eventsourced"
	"gochen/eventing"
	"gochen/eventing/projection"
	"gochen/examples/infra/projection/idempotent/internal/idemp"
	"gochen/messaging"
)

// ItemAdded 事件
type ItemAdded struct {
	SKU string
	Qty int
}

// 读模型：每个 SKU 的累计数量（内存演示）
var stock = map[string]int{}

// GetStock 返回指定 SKU 在内存读模型中的累计库存。
func GetStock(sku string) int { return stock[sku] }

// ItemAddedEnvelope 承载解码后的载荷 + 事件ID，用于幂等判断
type ItemAddedEnvelope struct {
	E  *ItemAdded
	ID string
}

// NewIdempotentProjection 创建一个带事件去重能力的库存投影。
func NewIdempotentProjection(sink *idemp.Sink) (projection.IProjection[int64], error) {
	return eventsourced.NewEventSourcedProjection[*ItemAddedEnvelope](eventsourced.EventSourcedProjectionOption[*ItemAddedEnvelope]{
		Name:       "stock_projection",
		EventTypes: []string{"ItemAdded"},
		Decoder: func(evt eventing.IEvent) (*ItemAddedEnvelope, error) {
			if evt == nil {
				return nil, nil
			}
			if p, ok := messaging.PayloadAs[*ItemAdded](evt.GetPayload()); ok {
				return &ItemAddedEnvelope{E: p, ID: evt.GetID()}, nil
			}
			return nil, nil
		},
		Handle: func(ctx context.Context, env *ItemAddedEnvelope) error {
			if env == nil || env.E == nil {
				return nil
			}
			if sink.Seen(env.ID) {
				return nil
			} // 幂等：已处理则跳过
			stock[env.E.SKU] += env.E.Qty
			sink.Mark(env.ID)
			return nil
		},
	})
}
