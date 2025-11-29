package domain

import (
	"context"

	"gochen/di"
	"gochen/eventing/bus"
	"gochen/eventing/projection"
)

// IModule 定义领域模块的最小契约。
//
// 设计目标：
//   - 让像 gochen-iam、gochen-llm 这样的独立领域模块可以被一个通用的 Server
//     以统一方式装配和运行；
//   - 与上层业务仓库（如 alife）中的 internal/di.IDomainModule 保持语义一致，
//     避免重复定义不同版本的接口。
//
// 约定：
//   - Name 用于日志与调试；
//   - RegisterProviders 仅注册仓储、服务、路由构造器等“提供者”；
//   - RegisterEventHandlers 负责订阅领域事件（可选）；
//   - RegisterProjections 负责注册读模型投影（可选）。
type IModule interface {
	// Name 返回领域模块名称
	Name() string

	// RegisterProviders 注册该领域的所有提供者（Repos、Services、路由构造器等）
	RegisterProviders(container di.IContainer) error

	// RegisterEventHandlers 注册该领域的事件处理器
	// 返回错误时视为启动失败
	RegisterEventHandlers(ctx context.Context, eventBus bus.IEventBus, container di.IContainer) error

	// RegisterProjections 注册该领域的投影（若有）
	// 返回投影管理器和投影名称列表；若无投影，可返回 (nil, nil, nil)
	RegisterProjections(container di.IContainer) (*projection.ProjectionManager, []string, error)
}

