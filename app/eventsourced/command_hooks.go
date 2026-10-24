package eventsourced

import (
	"context"
	"time"

	deventsourced "gochen/domain/eventsourced"
)

// IEventSourcedCommandHook 命令执行钩子接口。
// 可用于统计、审计、校验等横切逻辑。
type IEventSourcedCommandHook[T deventsourced.IEventSourcedAggregate[ID], ID comparable] interface {
	BeforeExecute(ctx context.Context, cmd IEventSourcedCommand[ID], agg T) error

	// AfterExecute 在一次命令执行尝试结束后调用。
	//
	// 注意：
	// - 参数 err 表示“本次尝试的最终错误”（可能来自加载聚合、BeforeExecute、handler 或保存阶段）；
	// - 当加载聚合失败时，agg 为 repository 返回值，可能是工厂创建的空实例，也可能是 T 的零值，hook 实现必须能处理该情况；
	// - 当某个 hook 的 BeforeExecute 返回错误时，后续 hook 的 BeforeExecute 不会再被调用，但它们仍会收到 AfterExecute；
	// - 当启用并发重试（ConcurrencyRetry）时，一个命令可能会触发多次尝试，因此该 hook 可能被调用多次。
	AfterExecute(ctx context.Context, cmd IEventSourcedCommand[ID], agg T, err error) error
}

// IEventSourcedCommandFinalizeHook 命令执行“最终一次”钩子接口（可选）。
//
// 用途：用于 metrics/审计/收尾等“只希望每个命令执行调用一次”的逻辑。
//
// 注意：
// - 每次 ExecuteCommand 只会调用一次（无论成功/失败/加载失败/BeforeExecute 失败/重试耗尽）；
// - 参数 err 表示“本次命令执行的最终错误”（成功则为 nil）；
// - 当加载聚合失败时，agg 为 repository 返回值，可能是工厂创建的空实例，也可能是 T 的零值，hook 实现必须能处理该情况；
// - attempts 表示“实际尝试次数”（包含首次执行尝试；重试耗尽时通常为 MaxRetries+1）。
type IEventSourcedCommandFinalizeHook[T deventsourced.IEventSourcedAggregate[ID], ID comparable] interface {
	AfterFinalize(ctx context.Context, cmd IEventSourcedCommand[ID], agg T, err error, attempts int) error
}

// ICommandTracer 提供命令执行过程的耗时与错误追踪。
type ICommandTracer interface {
	Trace(ctx context.Context, commandName string, elapsed time.Duration, err error)
}
