package eventsourced

import (
	"context"

	deventsourced "gochen/domain/eventsourced"
)

// IEventSourcedCommand 事件溯源命令接口（应用层）。
//
// 命令需要提供聚合 ID，用于定位目标聚合根。
//
// 类型参数：
//   - ID: 聚合根 ID 类型，必须是可比较类型（如 int64、string、uuid.UUID 等）
type IEventSourcedCommand[ID comparable] interface {
	AggregateID() ID
}

// EventSourcedCommandHandler 命令处理器函数类型。
// 处理器接收命令与聚合实例，并在聚合上执行业务逻辑。
type EventSourcedCommandHandler[T deventsourced.IEventSourcedAggregate[ID], ID comparable] func(ctx context.Context, cmd IEventSourcedCommand[ID], aggregate T) error
