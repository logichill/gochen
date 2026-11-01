# 分层授权

Gochen 将动作权限、业务隔离、数据范围和决策治理分别建模。Core 提供授权端口、规约代数与 Application 编排，Runtime 提供默认判定器和 SQL 规约执行。

## 能力与落点

| 层级 | 解决的问题 | 当前入口 |
| --- | --- | --- |
| L0 Plain | 基础用例编排 | `app/crud`、`app/audited`、`app/eventsourced` |
| L1 Action | 能否执行某个动作 | `auth/action`（`action.RBAC`） |
| L2 Isolated | 数据属于哪个业务隔离空间 | `contextx.TenantID`、`domain/crud.ITenantEntity`、Runtime Repo 的 `WithIsolation` |
| L3 Scoped | 能操作哪些范围内的哪些资源 | `auth/scoped`（`scoped.New`）、`auth.IAuthorizer`、Runtime Repo 写规约 |
| L4 Governed | 策略快照、决策审计与指标 | `auth/governance`、Runtime SQL store |

各能力按包隔离。`auth` 根包只提供唯一授权端口 `IAuthorizer` 与共享值对象；CRUD Application 在构造期接收 `auth.IAuthorizer`，在每个用例入口调用一次 `Authorize` 并把决策中的 `spec.Spec` 交给仓储执行。

## 隔离、范围与主体

Tenant 表达业务隔离，Scope 表达授权覆盖范围，资源的 `managed_scope_id` 表达管理归属。Scope ID 为 `int64`，隔离标识为 `string`，两者不能相互替代。

多身份业务应显式确定当前工作身份，再解析其可见范围。角色绑定、Scope 树、身份激活流程与可见集合的持久化由下游身份系统维护，Core 不提供这些业务表。上级范围对子树的可见性应由 resolver 返回完整集合，不能用 `managed_scope_id = active_scope_id` 代替。

`auth/scoped.DataScope` 包含 `Kind`、`ScopeIDs` 和 `TenantID`：

| 状态 | 构造函数 | 语义 |
| --- | --- | --- |
| `ScopeDenyAll` | `DenyAll()`，也是零值 | 拒绝读写 |
| `ScopeFiltered` | `Filtered(ids...)` | 限定于有效范围 ID 集合 |
| `ScopeGlobal` | `Global()` | 显式全局范围，仍受独立的 L2 隔离约束 |

范围归一化会去重、排序并去除非正数 ID；Filtered 的有效集合为空时转为 DenyAll。范围组合取交集，两个非空 TenantID 冲突时拒绝。Global 只能在受控的服务上下文中设置，不能从普通请求参数直接接受。

## 请求与写入链路

```text
Transport 身份认证 → contextx / Principal
                           ↓
      Action → Scoped → Application → Repository
                       ↓                       ↓
                  Authorizer            原子执行写规约
                       ↓
              Decision.Spec（spec.Spec）
```

1. Transport 根据已验证的身份建立上下文。公网请求不能直接把客户端 header 当作租户身份；可信网关头解析须显式配置 resolver。
2. Application（如 `app/crud.Application`）在每个用例入口调用一次 `auth.IAuthorizer.Authorize`，授权请求携带资源、动作与目标 ID。
3. 判定器返回 `auth.Decision`：allow 时为行级 `spec.Spec`，可选附带服务端赋值（`RoleScope` / `RoleOwner`）。deny 直接短路，用例不触达仓储。
4. Repository 把写规约合并进最终 SQL 的 `WHERE`（写入前另做 `WITH CHECK`）。规约缺失、非法或含 `Named` 条件时拒绝，不能以“先查后判断”替代原子写入。

有已知目标的读写由规约同时约束“查得到”和“改得动”；列表等无目标判定返回的规约只用于过滤结果集。不得伪造目标来满足校验，也不得在 Application 层先查后过滤。

Update 的授权输入来自仓储中的真实资源边界，不能信任客户端提交的 scope / tenant。`GetWithDeleted`、`Restore`、`Purge`、`ListDeleted` 均接收规约，软删记录的访问同样受规约约束。批量操作逐项匹配授权，任何越界项都应导致整批拒绝。普通更新不得悄然转移资源的隔离或管理归属。

## 仓储装配

隔离能力在构造期显式声明：

- `WithIsolation(IsolationCols{...})`：启用隔离；默认值来源为 `contextx.TenantID`。
- `WithResourceKind(kind)`：声明仓储对应的授权资源种类。
- `WithCrossIsolationReads(audit)`：声明该仓储可被请求期凭据临时开闸读取。
- `WithAudited()` / `WithoutVersioning()` / `WithMaxPageSize(n)`：审计、乐观锁与分页上限。

字段名或实体 tag 不会自动开启隔离保护。默认租户隔离要求实体实现 `ITenantEntity`；SQL 仓储通过列映射写入归属。自定义 `IsolationCols.Resolve` 必须同时显式指定 `Column`。列名覆盖顺序见 [Quick 装配](../guides/quick-assembly.md#列名约定)。

写规约由调用方（Application 或显式授权的服务）传入每个读写方法；仓储不维护隐式常驻约束，也不从 ctx 猜测写边界。`Create` / `Update` 在写入前用内存解释器校验规约（`WITH CHECK`），`Update` 以 `WHERE id ∧ spec ∧ version` 原子执行；访问控制列不进入 `SET`。

## 默认判定器

`gochen-runtime/host/authz` 提供 `NewActionChecker` 和 `NewEvaluator`，从显式注入的 Principal 判定动作与资源：

- 动作权限使用三段式码，如 `order:api:update`，支持按段通配。格式错误返回 `InvalidInput`。
- 无具体目标时判断动作；有目标时逐项核对隔离和管理范围。
- 数据范围优先取已绑定的 DataScope；系统主体可使用全局范围，否则从 `ActiveScopeID` 得到单范围。缺少有效范围时拒绝。
- 子树可见性通过 `WithEvaluatorDataScope(resolver)` 注入。Create 的目标范围必须由资源解析器明确给出；`ManagedScopeID=0` 的普通资源拒绝访问。
- `GlobalScope` 资源仅对全局可见主体开放。

业务可替换 `IEvaluator` 接入其他策略系统，保持 Application 与仓储链路不变。权限目录由组合根创建 `authz.Registry`，通过 Host 的 CatalogRegistrar 注入；模块用 `authz.Catalog` 声明权限。

## 跨隔离读取

Runtime Repo 的跨隔离读取同时要求构造期 `WithCrossIsolationReads(audit)` 和请求期 `contextx.WithCrossIsolation` 凭据，凭据必须包含 Reason 与 Operator。

- 缺少任一条件时保持普通隔离语义。
- 开启后只放行读取，写操作返回 `Forbidden`。
- 留痕失败即拒绝读取；写规约仍照常合并进查询条件。
- 要求写规约但无法保留有效边界时拒绝。

不要为同一张表另建无隔离仓储绕过这一入口。需要跨隔离写入时，按每个目标隔离键派生 context，并执行正常授权。

## 决策治理

`auth/governance.Wrap` 按 `Metrics(Log(Snapshot(base)))` 装饰 `scoped.IAuthorizer`。至少配置快照、审计或指标中的一项；显式传 nil clock / ID generator 会在构造期失败。

- 快照获取默认失败即拒绝，可通过 `WithLenientSnapshot` 显式放宽。
- 审计写入默认不阻塞授权，`WithStrictAudit` 可改为阻塞；可靠重试由 store 或外部队列负责。
- 指标失败不影响授权。审计交付前统一脱敏，可用资源脱敏器定制。

## 验证入口

Core 契约测试位于 `auth/`、`app/crud/`、`testkit/`；SQL 写规约与隔离测试位于 `gochen-runtime` 仓库 `db/orm/repo/`。内存仓储的版本与规约处理不能代表 SQL 列过滤的全部语义，接入 SQL 仓储时须验证真实查询条件。

可运行示例位于 `gochen-runtime` 仓库 `examples/security/layering` 和 `examples/quick`。
