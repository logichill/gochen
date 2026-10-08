package eventsourced

import (
	"gochen/app/operation"
	deventsourced "gochen/domain/eventsourced"
	"gochen/observe/logging"
)

// EventSourcedServiceOptions 事件溯源命令服务配置（应用层）。
type EventSourcedServiceOptions[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	Logger        logging.ILogger
	CommandHooks  []IEventSourcedCommandHook[T, ID]
	CommandTracer ICommandTracer

	// OperationRunner 为 ExecuteCommandWithOperation 提供 operation envelope 包装器（可选）。
	//
	// 未配置时 inline operation 使用 operation.DefaultRunner()；tracked operation 必须显式配置
	// 带持久化 store 的 runner，避免返回不可查询的 operation id。
	OperationRunner operation.IRunner

	// OperationSpecResolver 为 ExecuteCommandWithResolvedOperation 按命令解析 operation spec（可选）。
	OperationSpecResolver OperationSpecResolver[ID]

	// OperationResultResolver 为 ExecuteCommandWithResolvedOperation 构造业务 result（可选）。
	//
	// 未配置或返回 nil 时，默认使用命令聚合 ID 补齐 Resource.ID。
	OperationResultResolver OperationResultResolver[ID]

	// ConcurrencyRetry 配置“保存阶段遇到并发冲突（errors.Concurrency）”时的自动重试（可选）。
	//
	// 语义：
	// - 重试发生在 ExecuteCommand 内部：会重新加载聚合并重新执行 handler（而不是重放旧未提交事件）；
	// - 因此要求 handler 具备“可重入/幂等”特性：不要在 handler 内产生不可回滚的外部副作用。
	ConcurrencyRetry *RetryConfig

	// IsConcurrencyError 自定义“是否为并发冲突错误”的判断函数（可选）。
	//
	// 默认使用 errors.Is(err, errors.Concurrency)。
	IsConcurrencyError IsConcurrencyError
}
