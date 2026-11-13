# 分层授权

Gochen 将用例编排、动作权限、数据隔离、数据范围和决策治理分为 L0–L4。每一层回答一个问题，业务按需要组合这些能力。

例如，用户读取订单时：L1 检查是否拥有订单读取权限，L2 限定当前租户，L3 限定可见部门，L4 记录这次授权使用的策略版本、结果和耗时。L0 负责把授权、仓储和事务组成完整用例。

## 能力与落点

| 层级 | 解决的问题 | 当前入口 |
| --- | --- | --- |
| L0 Plain | 一个业务用例如何执行 | `app/crud`、`app/crud/audit`、`app/eventsourced` |
| L1 Action | 能否执行某个动作 | `auth.Compile`、`auth.NewPermissionPolicy` |
| L2 Isolated | 数据属于哪个业务隔离空间 | `contextx.TenantID`、Runtime Repo 的 `WithIsolation` |
| L3 Scoped | 能访问哪些范围内的数据 | `auth.ScopeRule`、`auth.WithScope`、项目范围解析器 |
| L4 Governed | 如何记录和治理授权决策 | `auth.GovernanceOptions`、Runtime SQL store |

这些层级说明职责。Application 统一接收 `auth.IAuthorizer`，组合根编译强类型声明，并通过 `GovernanceOptions` 配置治理；仓储独立执行配置好的隔离规则。

## L0 Plain：统一用例入口

Application 编排授权、校验、Hooks、事务和仓储操作。HTTP、Worker、CLI 调用同一个 Application，遵循同一套业务规则。

`crud.Options.Authorizer` 必须显式提供。常用选择为：

- `auth.Allow()`：明确允许应用动作，仓储的 L2 隔离继续生效。
- `auth.Compile(declaration, options)`：连接 Descriptor、字段引用、动作检查器及范围规则，构造冻结计划。
- `auth.New(options)`：将领域扩展策略与治理配置接入统一内核。

普通用例在入口授权一次。列表和计数用返回的条件过滤数据；已知 ID 的读写同样受该条件约束。批量写入共用一次授权和一个事务，提交前任一项失败时整批回滚。CRUD 与审计用例的校验、钩子和提交后错误处理见 [应用模板](../../app/README.md)。

## L1 Action：动作权限

L1 检查当前主体能否执行指定动作，例如 `order:api:read`、`order:api:update`。默认权限码为 `<resource>:api:<action>`，也可在动作声明中指定。

Core 的 `auth.IActionChecker` 统一动作检查；Runtime 的 `host/authz.NewActionChecker()` 从已认证主体的权限集合读取授权，支持按段通配。权限码格式错误返回 `InvalidInput`，缺少身份返回 `Unauthorized`，权限不足返回 `Forbidden`。

权限目录由组合根通过 Runtime `host/authz.Registry` 和模块 `Catalog` 登记。HTTP 的 `FastDeny` 可提前拒绝明显无权限的请求，Application 仍执行正式授权。

## L2 Isolated：业务隔离

Tenant 表达业务隔离空间。请求中的 `contextx.TenantID` 决定当前空间；仓储通过 `WithIsolation` 显式绑定隔离字段。

例如，租户 A 的订单查询带有 `tenant_id = 'A'`；创建时仓储盖戳归属，更新和删除也限定在 A。启用隔离后，缺少有效隔离键的访问会被拒绝。

`dim:"tenant"` 等字段标签描述实体，隔离能力由仓储构造选项开启。普通字段也可作为隔离字段，`auth.TenantEntity` 提供常用租户字段与方法。配置方式见 [资源契约](resource-contracts.md#隔离与资源装配)和 [Quick 装配](../guides/quick-assembly.md#列名约定)。

跨隔离报表需要两项明确授权：仓储配置 `WithCrossIsolationReads(audit)`，请求提供带 Reason 和 Operator 的 `contextx.WithCrossIsolation` 凭据。开启后仅允许读取；留痕失败阻断读取，L3 范围条件继续生效。跨隔离写入应从普通上下文按目标租户分别建立身份与范围，再执行正常授权。

## L3 Scoped：数据范围

Scope 表达当前身份的授权覆盖范围。例如，一个用户在租户 A 内可见部门 7、8，最终查询同时满足 `tenant_id = 'A'` 和 `department_id IN (7, 8)`。

声明的 `ScopeRule` 先检查动作权限，再由 `auth.ISpecResolver` 返回数据条件。低层策略使用 `auth.NewPermissionPolicy(checker, auth.WithScope(resolver))`。默认解析器 `host/authz.NewScopeResolver("department", resolver)` 将范围 ID 映射到实体的 `dim:"department"` 字段。

### DataScope 的三种状态

| 构造函数 | 状态 | 语义 |
| --- | --- | --- |
| `DenyAll()`，也是零值 | `ScopeDenyAll` | 可见集合为空 |
| `Filtered(7, 8)` | `ScopeFiltered` | 限定在范围 7、8 |
| `Global()` | `ScopeGlobal` | 当前隔离空间内的全局范围 |

范围 ID 为 `int64`，租户标识为 `string`。`Filtered` 去重、排序并去除非正数 ID；有效集合为空时成为 `DenyAll`。显式组合多个 DataScope 时取交集，非空 TenantID 冲突时拒绝。

`NewScopeResolver` 优先使用有效的已绑定 DataScope；未绑定时可由项目 `IDataScopeResolver` 解析，默认无自定义解析器时使用主体的 `ActiveScopeID`。缺少有效范围时返回空条件。上级范围对子树的可见性由项目解析器提供完整集合。

角色绑定、范围树和工作身份切换由 IAM 等业务系统维护。创建时的目标归属由项目 `ISpecResolver` 明确返回服务端赋值；默认 `NewScopeResolver` 负责范围条件。

### 身份与范围的绑定顺序

可信认证入口按以下顺序建立请求上下文：

1. 验证身份并写入当前 `contextx.TenantID`。
2. 用 Runtime `host/authz.WithPrincipal` 建立主体和权限资料。
3. 绑定 `scoped.WithDataScope`，以及需要的 active scope ID/kind。

Core `auth.Principal` 保存主体、租户、当前范围和 System；Runtime 认证入口从当前上下文填写根主体的 `TenantID`，并补充权限、角色与用户归属资料。Runtime Principal 的 `HomeTenantID` / `HomeScopeID` 保存用户原属租户与范围；权限、角色、`PermissionScopes` 和 `ActiveBindingID` 随当前身份绑定。`PermissionScopes` 限定动作权限可使用的授权域；`contextx.UserID` 用于追踪。

通过 `WithPrincipal` 切换主体、租户、当前范围或系统身份后，旧权限和范围资料失效；切回原身份仍需重新绑定。同一身份重绑定沿用原绑定，原上下文继续有效。后台任务先建立系统身份，再绑定任务所属范围。非空 `DataScope.TenantID` 必须等于当前租户，范围读取返回独立副本。

`AsSystem` 保留根身份绑定并记录发起人与原因，先前普通身份的范围资料失效。系统授权由可信服务入口建立，仓储的 L2 隔离继续生效。

## L4 Governed：决策治理

`auth.New` 与 `auth.Compile` 统一配置 `GovernanceOptions`：判定前解析版本，策略返回后校验决策，再记录审计与指标。零配置关闭治理。

- 策略快照：判定前解析版本。启用后版本必须非空，解析失败默认拒绝；`LenientSnapshot` 允许显式降级。
- 决策审计：记录结果、原因、版本和本次缓存诊断。默认尽力写入，`StrictAudit` 要求留痕成功。
- 指标：记录结果与耗时，指标失败保持原授权结果。

`Decision.SnapshotVersion` 记录解析到的版本。让实际策略评估使用该版本、控制权限撤销新鲜度和请求内版本固定，是策略接入方的职责。缓存 TTL 控制回源频率，同一上下文跨过 TTL 后可能读到新版本；`ConsistencyMode` 当前用于留痕声明。

显式使用 `auth.Allow()` 的资源带有放行标记。`auth.AllPolicies(...)` 的全部子策略使用 `AllowPolicy()` 时保留该标记。入口门禁使用编译计划中的 `AllowReason`，独立记录放行来源和理由。

## 一次请求如何执行

```text
可信认证入口 → 身份、租户、范围上下文
                       ↓
                Application（L0）
                       ↓
          动作或范围授权器（L1 / L3）
             可由治理层包装（L4）
                       ↓
                Decision.Spec
                       ↓
               Repository（L2）
                       ↓
           隔离条件 + 授权条件 + 写入校验
```

授权条件采用 `spec.Spec`，通过 `meta.Ref` 指向实体字段或维度。SQL 仓储将条件合入查询和原子写入；事件溯源 Application 在回放后的聚合上求值。授权器声明可引用的字段和可赋值的维度，Application 在构造期和决策返回时校验。具体规则见 [资源契约](resource-contracts.md)。

配置错误返回 `InvalidInput`，授权实现产生非法条件返回 `Internal`；合法拒绝返回 `Forbidden`，范围内不可见的实体返回 `NotFound`。普通更新保持受保护的隔离和归属字段；归属变更使用独立的 [Transfer 用例](resource-contracts.md#两阶段迁移)。

## 示例与验证

在 `gochen-runtime` 仓库运行 `GOWORK=off go run ./examples/security/layering`，可看到 L0–L4 的装配、租户隔离、范围过滤和跨隔离读取。SQL / REST 装配见该仓库 `examples/quick`。

Core 的 `auth/...`、`app/...`、`testkit/...` 和 Runtime 的 `host/authz/...`、`db/orm/repo/...` 覆盖授权行为；接入实际数据库时验证最终查询、条件写入和事务回滚。
