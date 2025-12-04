package eventsourced

import (
	"context"
	"time"

	"gochen/eventing/bus"
	"gochen/logging"
)

// IEventSourcedCommand 命令需要提供聚合 ID
type IEventSourcedCommand interface {
	AggregateID() int64
}

// EventSourcedCommandHandler 命令处理器
type EventSourcedCommandHandler[T IEventSourcedAggregate[int64]] func(ctx context.Context, cmd IEventSourcedCommand, aggregate T) error

// EventSourcedServiceOptions 服务配置
type EventSourcedServiceOptions[T IEventSourcedAggregate[int64]] struct {
	EventBus       bus.IEventBus
	Logger         logging.ILogger
	CommandHooks   []EventSourcedCommandHook[T]
	ICommandTracer ICommandTracer
}

// EventSourcedCommandHook 命令执行钩子
type EventSourcedCommandHook[T IEventSourcedAggregate[int64]] interface {
	BeforeExecute(ctx context.Context, cmd IEventSourcedCommand, agg T) error
	AfterExecute(ctx context.Context, cmd IEventSourcedCommand, agg T, execErr error) error
}

// ICommandTracer 提供执行耗时等指标记录
type ICommandTracer interface {
	Trace(ctx context.Context, commandName string, elapsed time.Duration, err error)
}
