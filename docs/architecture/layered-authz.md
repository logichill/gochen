# 分层授权

Gochen 将动作权限、业务隔离、数据范围和决策治理分别建模。Core 提供安全契约与 Application 装饰器，Runtime 提供标准装配、默认判定器和 SQL 约束执行。

## 能力与落点

| 层级 | 解决的问题 | 当前入口 |
| --- | --- | --- |
| L0 Plain | 基础用例编排 | `app/crud`、`app/audited`、`app/eventsourced` |
| L1 Action | 能否执行某个动作 | `auth/action`、`app/security/action` |
| L2 Isolated | 数据属于哪个业务隔离空间 | `contextx.TenantID`、`domain/crud.ITenantEntity`、Runtime Repo 的 `WithIsolation` |
| L3 Scoped | 能操作哪些范围内的哪些资源 | `auth/scoped`、`app/security/scoped`、Runtime Repo 的 `WithScope` |
| L4 Governed | 策略快照、决策审计与指标 | `auth/governance`、Runtime SQL store |

各能力按包隔离。`auth` 根包只提供命名空间说明；基础 Application 不依赖 `auth`。Event Sourced Application 当前可在命令入口使用 L1 middleware，Core 未提供对应的 L3 Application 装饰器。

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
                  Authorizer             原子执行约束
                       ↓
              DataScope / WriteConstraint
```

1. Transport 根据已验证的身份建立上下文。公网请求不能直接把客户端 header 当作租户身份；可信网关头解析须显式配置 resolver。
2. Runtime `security.Profile.Build` 固定外到内的 `Action(Scoped(app))` 顺序。HTTP、Worker、CLI 都调用同一受保护的 Application；HTTP `api/rest/action.FastDeny` 仅提前拒绝。
3. Scoped 通过 Authorizer 判定资源，将主体范围与决策范围取交集，并通过 `IConstraintProvider` 放入本次派生 context。
4. Repository 按资源类型读取约束，在最终 SQL 中合并隔离、范围、资源 ID 与 revision 条件。约束缺失、为空或类型不匹配时拒绝，不能以“先查后判断”替代原子写入。

写入或已知目标读取使用 `Decision.IsAllowed()`，它同时要求 allow 与非空授权资源。列表等无目标判定使用 `IsActionAllowed()`，随后由 DataScope 过滤；不得伪造占位资源来满足校验。

Update 的授权输入来自仓储中的真实资源边界，不能信任客户端提交的 scope / tenant。Restore、Purge、AuditTrail 需要包含软删记录的边界读取能力。批量操作逐项匹配授权，任何越界项都应导致整批拒绝。普通更新不得悄然转移资源的隔离或管理归属。

## 仓储装配

安全能力在构造期显式声明：

- `WithIsolation(IsolationCols{...})`：启用隔离；默认值来源为 `contextx.TenantID`。
- `WithResourceKind(kind)` 与 `WithScope(ScopeCols{...})`：声明受控资源和范围列。
- `WithRequireScope()`：要求可执行的数据边界。

字段名或实体 tag 不会自动开启隔离与范围保护。默认租户隔离要求实体实现 `ITenantEntity`；SQL 仓储通过列映射写入归属。自定义 `IsolationCols.Resolve` 必须同时显式指定 `Column`。列名覆盖顺序见 [Quick 装配](../guides/quick-assembly.md#列名约定)。

L3 构造期检查 `HasScopeDeclaration`、`ResourceKind` 和 `ValidateWriteConstraintSupport`；资源类型必须与装饰器一致。安全装饰器保留 Application 实际具有的 Batch / Audited 能力。Repository 使用构造选项，禁止包装窄 `IRepository` 导致可选能力丢失或被假冒。

写入未命中时，仓储按构造期 `WriteMissMode` 决定是否用诊断探针区分不存在、越界和版本冲突；探针只服务错误分类，不参与替代原子约束。

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
- 留痕失败即拒绝读取；资源授权边界探针仍执行隔离。
- L3 范围约束继续生效；要求边界但无法保留有效边界时拒绝。

不要为同一张表另建无隔离仓储绕过这一入口。需要跨隔离写入时，按每个目标隔离键派生 context，并执行正常授权。

## 决策治理

`auth/governance.Wrap` 按 `Metrics(Log(Snapshot(base)))` 装饰 Authorizer，再交给 Scoped。至少配置快照、审计或指标中的一项；显式传 nil clock / ID generator 会在构造期失败。

- 快照获取默认失败即拒绝，可通过 `WithLenientSnapshot` 显式放宽。
- 审计写入默认不阻塞授权，`WithStrictAudit` 可改为阻塞；可靠重试由 store 或外部队列负责。
- 指标失败不影响授权。审计交付前统一脱敏，可用资源脱敏器定制。

## 验证入口

Core 契约测试位于 `auth/`、`app/security/`、`testkit/`；SQL 约束与隔离测试位于 `gochen-runtime` 仓库 `db/orm/repo/`。内存仓储的版本与范围处理不能代表 SQL 列过滤的全部语义，接入 SQL 仓储时须验证真实查询条件。

可运行示例位于 `gochen-runtime` 仓库 `examples/security/layering` 和 `examples/quick`。
