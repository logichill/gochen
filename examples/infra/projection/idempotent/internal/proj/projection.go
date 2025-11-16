package proj

import (
	"context"

	"gochen/domain/eventsourced"
	"gochen/eventing"
	"gochen/eventing/projection"
	"gochen/examples/infra/projection/idempotent/internal/idemp"
)

// ItemAdded 事件
type ItemAdded struct {
	SKU string
	Qty int
}

// 读模型：每个 SKU 的累计数量（内存演示）
var stock = map[string]int{}

func GetStock(sku string) int { return stock[sku] }

// ItemAddedEnvelope 承载解码后的载荷 + 事件ID，用于幂等判断
type ItemAddedEnvelope struct {
	E  *ItemAdded
	ID string
}

// NewIdempotentProjection 创建带幂等保护的投影
func NewIdempotentProjection(sink *idemp.Sink) (projection.IProjection, error) {
	return eventsourced.NewEventSourcedProjection[*ItemAddedEnvelope](eventsourced.EventSourcedProjectionOption[*ItemAddedEnvelope]{
		Name:       "stock_projection",
		EventTypes: []string{"ItemAdded"},
		Decoder: func(evt eventing.Event) (*ItemAddedEnvelope, error) {
			if p, ok := evt.Payload.(*ItemAdded); ok {
				return &ItemAddedEnvelope{E: p, ID: evt.ID}, nil
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
