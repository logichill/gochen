package eventsourced

import (
	"context"

	"gochen/app/operation"
)

// OperationSpecResolver 基于事件溯源命令解析 operation spec。
// 返回 nil 表示本次命令不需要 operation envelope，调用方退化为普通 ExecuteCommand 语义。
type OperationSpecResolver[ID comparable] func(ctx context.Context, cmd IEventSourcedCommand[ID]) (*operation.Spec, error)

// OperationResultResolver 在命令成功执行后构造 operation result。
// 返回 nil 时服务会使用聚合 ID 构造默认资源结果。
type OperationResultResolver[ID comparable] func(ctx context.Context, cmd IEventSourcedCommand[ID], spec *operation.Spec) (*operation.Result, error)
