package assembly

import (
	"context"
	"reflect"

	auth "gochen/auth"
	"gochen/di"
	"gochen/host/module"
	"gochen/httpx"
)

// Lifetime 注册生命周期类型。
type Lifetime = module.Lifetime

const (
	// SingletonLifetime 单例生命周期。
	SingletonLifetime = module.SingletonLifetime
	// TransientLifetime 瞬态生命周期。
	TransientLifetime = module.TransientLifetime
)

// Role 显式角色标记——不再靠反射推断。
type Role int

const (
	// RoleNone 无特殊角色（普通服务）。
	RoleNone Role = iota
	// RoleRouteRegistrar 路由注册器角色。
	RoleRouteRegistrar
	// RoleEventHandler 事件处理器角色。
	RoleEventHandler
	// RoleProjection 投影角色。
	RoleProjection
	// RoleRuntimeComponent 运行期组件角色。
	RoleRuntimeComponent
)

// Registration 是带运行期角色的显式 DI 注册项。
type Registration struct {
	// Lifetime 注册生命周期（单例/瞬态）。
	Lifetime Lifetime

	// ServiceType 显式 service type。
	ServiceType reflect.Type

	// Factory 工厂函数。
	// 与 Instance 互斥：两者只能设置其一。
	Factory di.Factory

	// Instance 直接实例（跳过构造器）。
	// 与 Factory 互斥：两者只能设置其一。
	Instance interface{}

	// Roles 显式角色标记。
	// 用于在 Init/RegisterRoutes/Start 阶段分类处理。
	Roles []Role
}

// ModuleDescriptor 显式模块装配描述符。
//
// 该描述符是 host.Module(...) builder 背后的实现模型；常规模块应通过
// host.Module(...) 声明，不需要直接构造该类型。
type ModuleDescriptor struct {
	// ID 模块的稳定标识。
	ID string

	// Name 模块的展示名称。
	Name string

	// Registrations 显式注册列表。
	Registrations []Registration

	// Permissions 模块声明的权限码目录。
	Permissions []string

	// PermissionDefinitions 模块声明的权限元数据目录。
	PermissionDefinitions []auth.PermissionDefinition

	// ResourceResolvers 模块声明的资源解析器目录。
	ResourceResolvers []auth.ITypedResourceResolver

	// OnInit 初始化钩子（可选）。
	OnInit func(opts module.ModuleInitOptions) error

	// Middlewares 模块级 HTTP 中间件。
	Middlewares []httpx.Middleware

	// OnStart 启动钩子（可选）。
	OnStart func(ctx context.Context) error

	// OnStop 停止钩子（可选）。
	OnStop func(ctx context.Context) error
}
