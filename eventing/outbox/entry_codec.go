package outbox

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
)

func EventToOutboxEntry[ID comparable](aggregateID ID, event eventing.Event[ID]) (*OutboxEntry[ID], error) {
	eventData, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	return &OutboxEntry[ID]{
		AggregateID: aggregateID,
		// 以下字段在 SQL Outbox 表结构中是核心索引/约束列：
		// - AggregateType：用于聚合维度过滤与诊断
		// - EventID：用于去重（unique）
		AggregateType: event.GetAggregateType(),
		EventID:       event.GetID(),
		EventType:     event.GetType(),
		EventData:     string(eventData),
		Status:        OutboxStatusPending,
		CreatedAt:     time.Now(),
	}, nil
}

// ToEventWith 转换事件。
func (entry *OutboxEntry[ID]) ToEventWith(
	reg *registry.Registry,
	upgraders *upcast.UpgraderRegistry,
) (eventing.Event[ID], error) {
	return entry.toEventWithContext(contextx.Background(), reg, upgraders)
}

func (entry *OutboxEntry[ID]) toEventWithContext(
	ctx context.Context,
	reg *registry.Registry,
	upgraders *upcast.UpgraderRegistry,
) (eventing.Event[ID], error) {
	var event eventing.Event[ID]
	decoder := json.NewDecoder(strings.NewReader(entry.EventData))
	decoder.UseNumber() // 关键：数字保持为 json.Number 而非 float64
	if err := decoder.Decode(&event); err != nil {
		return eventing.Event[ID]{}, err
	}

	if reg == nil {
		return eventing.Event[ID]{}, errors.NewCode(errors.InvalidInput, "event registry cannot be nil")
	}
	if upgraders == nil {
		return eventing.Event[ID]{}, errors.NewCode(errors.InvalidInput, "event upgrader registry cannot be nil")
	}

	if !reg.HasEvent(event.GetType()) {
		return eventing.Event[ID]{}, errors.NewCode(errors.NotFound, "unknown event type").
			WithContext("event_type", event.GetType()).
			WithContext("event_id", event.GetID())
	}

	// 对已注册的事件类型执行数据升级与强类型反序列化，
	// 确保下游 handler 可以按领域事件类型进行断言。
	upgraded, err := upcast.UpgradeEventPayload(ctx, reg, upgraders, &event)
	if err != nil {
		return eventing.Event[ID]{}, err
	}
	return *upgraded, nil
}
