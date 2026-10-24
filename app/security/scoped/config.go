package scoped

import (
	"gochen/auth/action"
	authscoped "gochen/auth/scoped"
)

// Config 描述 L3 装饰所需的编排配置。
type Config struct {
	// EntityType 是写约束投放的目标实体标识（如 "order"）。
	//
	// Repo 以同一标识读取约束；Hook 内写其他实体时匹配不到，
	// 从而被 fail-closed 拦截（嵌套写防护）。
	EntityType string

	// Policy 把 CRUD 操作映射到三段式动作码。
	Policy action.OperationPolicy

	// ScopeResolver 解析读路径的数据范围。
	//
	// 优先使用 ctx 已绑定的范围，再调用 resolver；两者均无时要求决策提供范围。
	ScopeResolver authscoped.IDataScopeResolver
}
