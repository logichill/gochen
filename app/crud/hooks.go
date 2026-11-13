package crud

import "gochen/app/internal/execution"

// 标准动作名。
const (
	ActionRead        = execution.ActionRead
	ActionList        = execution.ActionList
	ActionCreate      = execution.ActionCreate
	ActionUpdate      = execution.ActionUpdate
	ActionDelete      = execution.ActionDelete
	ActionRestore     = execution.ActionRestore
	ActionPurge       = execution.ActionPurge
	ActionListDeleted = execution.ActionListDeleted
)

// Stage 是单个动作的生命周期钩子。
type Stage[T any] = execution.Stage[T]

// Hooks 按动作名键控生命周期钩子；标准动作使用 read / list / create /
// update / delete / restore / purge / list_deleted，自定义动作
// 使用调用方声明的动作名。
type Hooks[T any] = execution.Hooks[T]
