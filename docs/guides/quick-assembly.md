# Quick 装配与列名约定

`gochen-runtime/quick` 提供 Repo、Application、REST 和 Host 的常用装配入口。它调用对应模块的正式实现，安全能力仍需明确声明。复杂配置使用 Runtime 的 Repo options、`security.Profile` 和 Host builder。

## 仓储与 Application

| 入口 | 输入与行为 |
| --- | --- |
| `quick.IsolatedRepo[T, ID](orm, table, opts...)` | 注入 `WithIsolation(IsolationCols{})`，启用 L2 租户隔离 |
| `quick.ScopedRepo[T, ID](orm, table, kind, opts...)` | 注入 `WithResourceKind`、`WithRequireScope`、`WithScope(ScopeCols{})` |
| `quick.AuditedRepo[T, ID](orm, table, opts...)` | 注入 `WithAudited`，声明审计字段和软删除能力 |
| `quick.App[T, ID](repository)` | 返回基础 `*crud.Application[T, ID]` |
| `quick.ActionApp(app, checker, prefix)` | 装饰已有 Application，派生 CRUD 动作码 |
| `quick.ActionAppFromRepo(repository, checker, prefix)` | 创建基础 Application 后启用 L1 |
| `quick.ScopedApp(app, kind, scopeFn, opts...)` | 装饰已有 Application，装配资源解析器、范围解析器与 Authorizer |
| `quick.ScopedAppFromRepo(repository, kind, scopeFn, opts...)` | 创建基础 Application 后启用 L3 |

上述构造函数均返回 error。`scopeFn` 的类型为 `func(T) int64`，必须返回资源实际管理范围。`ScopedRepo` 只开启 L3；需要 L2 时另外传入 `repo.WithIsolation`。`AuditedRepo` 不会创建审计 Store 或审计 Application，完整审计链路仍需 `app/audited`。

Scoped 默认从 Principal 的 ActiveScopeID 解析单范围，系统主体使用全局范围；没有有效身份时拒绝。动作前缀 `order` 派生 `order:api:create/read/update/delete/list`。可通过 `WithScopedPolicy`、`WithScopedScopeResolver`、`WithScopedEvaluator`、`WithScopedActionChecker`、`WithScopedAuthorizer` 定制策略；恢复、物理删除、审计查询等额外动作须显式配置对应权限。

## 列名约定

Core [db.NamingConvention](../../db/naming.go) 定义默认列名：

| 用途 | 字段 | 默认列名 |
| --- | --- | --- |
| 隔离 | `TenantColumn` | `tenant_id` |
| 管理范围 / 所有者 | `ScopeColumn` / `OwnerIDColumn` | `managed_scope_id` / `owner_id` |
| 乐观锁 | `VersionColumn` | `version` |
| 创建审计 | `CreatedAtColumn` / `CreatedByColumn` | `created_at` / `created_by` |
| 更新审计 | `UpdatedAtColumn` / `UpdatedByColumn` | `updated_at` / `updated_by` |
| 软删除 | `DeletedAtColumn` / `DeletedByColumn` | `deleted_at` / `deleted_by` |

列名按“Repo 显式 option → ORM / Database 的 NamingConvention → 内置默认值”覆盖。配置可经 `config.Provider.Load` 写入 `db.DBConfig.Naming`，由 `stdsql.New` 和 `lite.New` 传递；也可通过 `lite.WithNamingConvention` 指定 ORM 约定。

命名约定只提供列名，不负责开启安全能力。必须先调用 `WithIsolation` / `WithScope` 或对应 Quick 工厂；启用后的列必须能映射到实体。自定义 `IsolationCols.Resolve` 时必须同时显式指定 `Column`，不能借用默认租户列。

## REST 与 Host

| 入口 | 行为 |
| --- | --- |
| `quick.RESTRegistrar(app, opts...)` | 返回 `func(httpx.IRouteGroup) error`，用于模块路由注册 |
| `quick.RESTRegister(group, app, opts...)` | 直接调用标准 REST 注册器 |
| `quick.Module(name, registrar, opts...)` | 返回 `module.ModuleCtor`，用于轻量路由模块 |
| `quick.New(opts...)` | 创建应用声明，高级选项复用 host/config |
| `app.Modules(ctors...)` | 注册模块，默认按模块 ID 推导前缀 |
| `app.Group(prefix).Use(middlewares...)` | 创建共享前缀与 middleware 的作用域，可继续嵌套 |
| `app.Mount(prefix, ctor)` | 显式覆盖模块前缀，"/" 表示当前组根路径 |
| `app.Run(ctx)` / `app.Build()` | 阻塞运行 / 返回生命周期引擎 |

Quick Host 默认监听 `0.0.0.0`，BasePath 为 `/api/v1`，使用 net/http server，并启用路由冲突检查。`quick.Module` 可通过 `WithMiddlewares`、`WithDependencies`、`WithExtensions` 配置模块行为；它不提供业务 provider builder。需要依赖注入 provider、事件处理器或投影注册时，使用 `gochen-runtime/host.Module`。

`registrar` 的类型为 `func(httpx.IRouteGroup) error`。对象通过 `router.RegisterRoutes` 方法值传入；需要工厂初始化时，先在调用方完成构造和错误处理，再传入注册函数。

需要自定义监听地址、BasePath、HTTP server 或运行时能力时，将 `host/config` options 传给 `quick.New`。基础设施由组合根创建，再交给业务模块；完整模块和轻量模块使用相同的挂载入口。

```go
app := quick.New()
app.Use(auditMiddleware)
app.Modules(NewIAMModule)
business := app.Group("").Use(authMiddleware)
business.Modules(NewFamilyModule, NewShopModule)
business.Group("/admin").Modules(NewLLMModule)
if err := app.Run(ctx); err != nil {
    return err
}
```

默认路径为 BasePath + 分组前缀 + "/" + 模块 ID；Mount 仅覆盖最后的模块前缀，不影响 middleware。模块 ID 仍在整个 Host 中唯一，不允许通过不同分组重复挂载同一个 ID。

middleware 顺序为基础 API → 外层分组 → 内层分组 → 模块 → registrar 局部；退出顺序相反。根级 Use 包含健康检查路由，分组 Use 只影响该组及子组模块。认证与授权策略必须显式声明，框架不根据模块名称猜测。

分组声明在 Build 时固定，构建前追加的 Use 会应用到该组已经声明的模块。每个 registrar 获得独立子 group，局部 Use 不影响同模块内的其它 registrar。路由注册后的 middleware 保持 HTTP 适配器原有的快照语义。

应用声明用于启动前单线程装配，每个 Application 只能 Build/Run 一次。模块工厂在 Host Prepare 时执行，模块依赖、扩展及生命周期能力保持不变。Quick 不接受 WithModules、WithModuleHTTP 或非空 WithModuleHTTPDefaults：使用 Modules、Mount、Group 声明，避免两套设置互相覆盖。底层 Host 仍提供显式配置能力。

## 可运行示例

在 `gochen-runtime` 仓库执行：

```bash
GOWORK=off go run ./examples/quick/modules
```

无需配置文件，访问 `/api/v1/hello/ping` 或 `/api/v1/admin/hello-admin/ping`，Ctrl-C 优雅退出。

数据库与授权装配示例：

```bash
GOWORK=off go run ./examples/quick
```

该示例使用内存 SQLite 数据库，演示列名覆盖、L2 + L3 仓储、受保护 Application、REST 与 Host。应用分层见[下游接入指南](downstream-guide.md)，授权规则见[分层授权](../architecture/layered-authz.md)。
