package eventsourced

import (
	"context"
	"time"

	"gochen/logging"
)

// IEventSourcedCommand 事件溯源命令接口。
//
// 命令需要提供聚合 ID，用于定位目标聚合根。
//
// 设计说明：
// - AggregateID() 返回 int64 类型，与项目中聚合根的 ID 类型保持一致
// - int64 能够兼容 Snowflake ID、数据库自增 ID 等常见场景
// - 如果业务需要使用其他 ID 类型（如 UUID），建议：
//   1. 在命令中同时保存原始 ID 和转换后的 int64 ID
//   2. 或在应用层实现 ID 映射逻辑
type IEventSourcedCommand interface {
	AggregateID() int64
}

// EventSourcedCommandHandler 命令处理器函数类型。
// 处理器接收命令与聚合实例，并在聚合上执行业务逻辑。
type EventSourcedCommandHandler[T IEventSourcedAggregate[int64]] func(ctx context.Context, cmd IEventSourcedCommand, aggregate T) error

// EventSourcedServiceOptions 事件溯源服务配置。
// 注意：该配置仅依赖领域与日志抽象，不依赖具体 EventStore 或 EventBus。
type EventSourcedServiceOptions[T IEventSourcedAggregate[int64]] struct {
	Logger         logging.ILogger
	CommandHooks   []EventSourcedCommandHook[T]
	ICommandTracer ICommandTracer
}

// EventSourcedCommandHook 命令执行钩子接口。
// 可用于统计、审计、校验等横切逻辑。
type EventSourcedCommandHook[T IEventSourcedAggregate[int64]] interface {
	BeforeExecute(ctx context.Context, cmd IEventSourcedCommand, agg T) error
	AfterExecute(ctx context.Context, cmd IEventSourcedCommand, agg T, execErr error) error
}

// ICommandTracer 提供命令执行过程的耗时与错误追踪。
type ICommandTracer interface {
	Trace(ctx context.Context, commandName string, elapsed time.Duration, err error)
}

