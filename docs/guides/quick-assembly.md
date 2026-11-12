# Quick 装配与列名约定

`gochen-runtime/quick` 提供 Repo、Application、事件溯源、REST 和 Host 的常用装配入口。它调用对应模块的正式实现，安全能力仍需明确声明。复杂配置使用 Runtime 的 Repo options、`auth.IAuthorizer` 实现与 Host builder。

## 仓储与 Application

| 入口 | 输入与行为 |
| --- | --- |
| `quick.IsolatedRepo[T, ID](orm, table, opts...)` | 注入 `WithIsolation(IsolationCols{})`，启用 L2 租户隔离 |
| `quick.SoftDeleteRepo[T, ID](orm, table, opts...)` | 注入 `WithSoftDelete`，仅声明软删除过滤 |
| `quick.App[T, ID](repository, options)` | 返回基础 `*crud.Application[T, ID]`；授权器由 `options.Authorizer` 提供 |
| `quick.DeclaredApp[T, ID](repository, options, rules, dependencies)` | 使用仓储元数据编译强类型规则，自动构造并绑定授权器 |

上述构造函数均返回 error。声明式应用使用 `DeclaredApp`，动作来自 `crud.Describe`，字段来自仓储有效契约。复杂装配可通过 `auth.Compile` 或 `auth.New` 构造授权器，再注入 `crud.Options.Authorizer`。`SoftDeleteRepo` 独立声明仓储能力，审计写入由 crud.Options.Audit 单独配置，历史读取由 app/audit.Reader 提供。

权限码来自 `auth.Descriptor`，默认为 `<resource>:api:<action>`；Action 检查从 ctx 中的 Principal 读取权限集合。声明的 `ScopeRule` 在动作权限成功后由 `auth.ISpecResolver` 产出范围条件与服务端赋值；系统行为通过规则的 `System` 字段明确配置。Application 自动把条件交给仓储，在最终 SQL 中执行 `WITH CHECK` 与 `WHERE`。

基础 CRUD Application 提供查询和批量用例；独立 deletion.NewApplication 要求恢复、已删除查询和永久删除，可选配置 Audit。REST 使用显式 WithDeletion / WithAudit 组合各能力，分别根据实际 Descriptor 推导路由，见 `gochen-runtime` 仓库 `api/rest/README.md`。

租户隔离和数据范围按 [L0–L4](../architecture/layered-authz.md)组合；字段保护、服务端赋值和 Transfer 通过 [资源契约](../architecture/resource-contracts.md)配置。

## 事件溯源

| 入口 | 输入与行为 |
| --- | --- |
| `quick.EventSourced(opts)` | 组合领域事件存储与单聚合仓储 |
| `quick.AtomicEventSourced(opts)` | 组合原子领域事件存储与批量仓储；要求 PersistOnly 和原子后端 |

两者接收 `quick.EventSourcedOptions[T, ID]`，均返回 error。`Store` 使用 Core 的 `eventstore.DomainEventStoreOptions[ID]`；聚合类型只在 `Store.AggregateType` 声明一次。`Sample`、`Factory`、`MetadataRegistry` 沿用领域仓储配置。以下片段假设已定义 BankAccount 聚合、事件注册表和工厂：

```go
repository, err := quick.EventSourced(quick.EventSourcedOptions[*BankAccount, int64]{
    Store: esstore.DomainEventStoreOptions[int64]{
        AggregateType:    "BankAccount",
        PublishMode:      esstore.PublishModePersistOnly,
        EventStore:       memoryStore,
        EventIDGenerator: uuid.NewGenerator(),
        EventRegistry:    eventRegistry,
        UpgraderRegistry: upgraders,
    },
    Sample:           &BankAccount{},
    Factory:          factory,
    MetadataRegistry: metadataRegistry,
})
if err != nil {
    return err
}
```

发布模式没有隐式默认值；Direct、Outbox 仍须提供对应依赖，见[存储适配契约](../../app/eventsourced/eventstore/README.md)。普通入口不暴露原子批量能力；原子入口在模式或后端不支持时于构造期报错。`Store.SnapshotManager` 只负责恢复，保存后创建快照仍需显式装配快照仓储装饰器。

完整可运行示例位于 Runtime 仓库：`go run ./examples/domain/eventsourced`。它使用内存存储，演示命令、保存、恢复与领域规则拒绝，无需数据库或后台服务。

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

命名约定只提供列名，不负责开启安全能力。必须先调用 `WithIsolation` 或对应 Quick 工厂；启用后的列必须能映射到实体。自定义 `IsolationCols.Resolve` 时必须同时显式指定 `Column`，不能借用默认租户列。

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

## typed Module 声明

Module 是模块参与系统的资源声明中心，不是运行期服务定位器。Repository、Application、Routes 在模块 ctor 中显式创建一次：

```go
return host.Module("notes").
    Name("笔记").
    Application(application).
    RouteRegistrar(module.RouteRegistrarFunc(quick.RESTRegistrar[*Note, string](application))).
    Build()
```

`Application` 接受 `app.IDescriptorProvider`，非 HTTP 用例也会登记。Routes 引用同一个应用，不复制动作或权限声明。`DependsOn` 只声明启动顺序；`PublicFeatures` 只声明可公开能力。角色入口接收实例，不再接收工厂函数。`RuntimeComponent` 必须使用非 nil 指针，以明确同一 Host 内唯一的 Start/Stop 所有者。不同类型相同或同类型不同对象都不是身份相同；同一对象的事件订阅与 Worker 生命周期分别回收。

`Projection(module.ProjectionOf[ID](projection))` 只做类型化声明，真实管理器仍在实际注册时检查 ID 类型，不支持的 ID 在启动时失败并回滚。提供应用的描述在构造期冻结，模块 Build 复制声明集合，Host 在路由核对后封存应用登记；自定义 Extension 的内部可变状态由自身契约管理。

`quick.Module` 只是将路由函数转换为这个正式模块，没有第二份生命周期实现。高级模块通过 `module.ApplicationRegistryFrom(initOptions)` 取得窄登记端口，不导入内部 Registry。详细迁移和不变边界见 [核心候选迁移](core-next-migration.md)。

完整普通模块示例：在 Runtime 执行 `GOWORK=off go run ./examples/typed-module`。它显式初始化独立 SQLite 内存表，并提供真实 HTTP CRUD、权限拒绝、版本冲突与关闭测试，监听本机回环地址。示例认证头仅作本机演示，不能用于生产。

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
