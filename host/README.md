# host：应用启动与模块装配入口

`gochen/host` 是下游项目的标准入口，负责收敛模块声明、模块化 Host、生命周期 Engine 与运行期装配。常规项目只需要理解两件事：

- `host.Module(...)`：声明一个模块有哪些 provider、路由、事件处理器、投影、后台组件与权限目录。
- `host.Run(...)` / `host.New(...)`：启动一组模块，并统一接入 HTTP、DI、事件、投影、消息与生命周期治理。

低层实现仍按职责分包维护，但不作为常规接入路径展示：

- `host/engine`：生命周期编排实现。
- `host/module`：基础模块模型与模块目录。
- `host/module/runtime`：默认模块化 Host 实现。
- `host/module/assembly`：`host.Module(...)` builder 背后的模块装配实现。
- `host/module/runtimecap`：模块初始化阶段的 HTTP、authz、event、projection 等运行时能力 accessor。
- `host/module/initcap`：`runtimecap` 等高级包使用的 typed key/value 存储。

## 推荐写法

模块声明：

```go
func NewModule() (host.IModule, error) {
	return host.Module("iam").
		Name("IAM").
		Provide(
			NewUserRepository,
			NewUserService,
		).
		RouteRegistrar(NewUserRoutes).
		PermissionDefinitions(PermissionDefinitions()...).
		Build()
}
```

应用启动：

```go
func main() {
	if err := host.Run(context.Background(),
		host.WithName("erp"),
		host.WithModules(
			iam.NewModule,
			inventory.NewModule,
			order.NewModule,
		),
		host.WithHTTPServer(httpServer),
		host.WithBasePath("/api/v1"),
		host.WithModuleHTTP("iam", host.ModuleHTTPConfig{Prefix: "/iam"}),
		host.WithFailFastOnRouteConflicts(true),
	); err != nil {
		os.Exit(1)
	}
}
```

## 生命周期

`host.Run(ctx, ...)` 的固定顺序：

1. 加载 Host 配置。
2. 准备 DI、HTTP、EventBus、Transport、ProjectionManager 等运行时能力。
3. 构建模块并按依赖拓扑排序。
4. 执行模块 `Init`。
5. 注册模块路由并启动后台组件。
6. 阻塞运行主 HTTP 服务。
7. 收到退出信号后逆序停止模块并关闭外围资源。

## 约束

- 组合根仍是依赖唯一真相源；DB、HTTP server、transport、event bus 等基础设施通过 `host.WithXxx(...)` 注入。
- 模块内部不要把 DI 容器当 Service Locator 使用；provider 由 `host.Module(...).Provide(...)` 声明。
- 普通业务模块不要直接构造 descriptor、registration、role；确需读取运行时能力时，通过 `host/module/runtimecap` 的 typed accessor 读取。
- 自定义生命周期应用可使用 `host.NewEngine(app, ...)`；这是高级入口，模块化应用优先使用 `host.Run(...)`。
