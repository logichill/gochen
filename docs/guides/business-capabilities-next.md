# next：业务能力、独立审计与显式 ES

本候选统一 CRUD 与删除生命周期，审计写入保持可选，历史读取独立，ES 保持权威事件及恢复路径。CQRS 中间态继续暂缓。

## 1. 统一资源生命周期与独立审计

- `crud.NewReadApplication` 返回真正只读的 `*crud.ReadApplication`，没有写入、执行、Transfer、Restore、Purge、ListDeleted 或 AuditTrail 方法。
- `crud.NewApplication` 是唯一通用资源门面，`Options.DeleteMode` 必须显式设为 `crud.DeleteSoft` 或 `crud.DeletePurge`。零值、未知值和不支持的软模式均在 Describe/构造期失败，绝不猜测或降级。
- `DeleteSoft` 下 Delete/DeleteBulk 只标记软删；重复删除可返回 NotFound/Conflict，但绝不升级为永久清除。`DeletePurge` 下 Delete/DeleteBulk 进入完整 Purge 管线，使用 purge 权限、钩子和审计。Purge 始终物理清除（包含旧软删记录）。内部共享算法，不重复生命周期实现。
- `EnableRestore` 与 `EnableDeletedQuery` 独立配置，软实体可以在硬模式下管理旧软删数据。仓储恰好有方法不会自动开启能力。HTTP 暴露另行选择。
- 所有写应用需要真实 `domain/crud.IPurgeRepository`。软实体需要独立 `IDeletedReadRepository.GetWithDeleted`，永久清除不要求 Restore。启用恢复再要求 `IRestoreRepository.Restore`，启用列表再要求 `IDeletedQueryRepository`。不存在补空方法的绕过适配器。
- `Options.Audit` 只选择同源事务审计写入，不强制历史查询、删除状态或操作者字段。`app/crud/audit.NewReader` 独立绑定历史查询、有效元数据与 audit_read 授权；永久删除以后仍按历史 DimensionSnapshot 查询。

`domain.ChangeInfo` 与 `domain.DeletionState` 可独立组合。`domain/crud.TrackedEntity` 只是保留原字段形状的便利组合。根 audit 的记录和存储契约、app/internal/execution 的统一执行协议保持原位。

### 破坏性迁移

已删除独立 `app/crud/deletion` 包、Application、Options、Describe 与 IDeletionApplication，不保留别名或转发。旧 deletion.NewApplication 改为 crud.NewApplication，并显式配置 DeleteMode 和可选恢复/查询标志；旧 app/deletion 同样不能继续使用。历史读取仍使用 app/crud/audit，根 gochen/audit 路径不变。

所有写入构造必须补齐显式 DeleteMode，不能把旧省略值当作硬删除默认值。硬模式标准描述只有 purge，无 delete；旧 delete 的 ActionPermissions、Hooks 需显式迁移到 purge，否则构造失败。软模式保留 delete，同时具有独立 purge。Describe 与 Application 使用同源配置，DeleteAction 返回冻结的实际动作，AuthorizationBinding/FastDeny 与真实管线一致。

restore / purge / delete / list_deleted / audit_read 是保留动作名，自定义 Actions 不能重新声明（包括空白变体）。四种通用 Execute 入口不能借用生命周期权限执行任意业务写入；普通 update 仍可用于 REST PUT，但输入、回调、Before 与校验后必须保持加载的删除状态与删除归因不变；创建入口必须产出未删除状态。任何违反均为 Forbidden，批量整批回滚，专用生命周期入口才可迁移删除态。

## 2. REST 与 Module 显式选择路由

默认公开普通 CRUD；恢复、永久清除和回收站路由必须显式经 `rest.WithRoutes` 选择，显式列表替换默认路由，可对完整资源只公开读取。`rest.WithAudit(history)` 独立组合历史读取，`WithDeletion` 已删除。方法集或实现 ISoftDeletable 本身不发布路由。

业务 DELETE 路由使用冻结 DeleteAction：软模式绑定 delete，硬模式绑定 purge。显式永久清除路径仍是 DELETE /:id/purge；恢复 POST /:id/restore，回收站 GET /deleted。各路由校验实际 Descriptor 和授权来源，不允许调用方另配模式或动作。纯删除状态不要求 operator；事务审计要求可信 operator。修改归因继续沿用仓储上下文 / DefaultActor（默认 system，不等于认证用户）。

## 3. 命名业务与一次动作绑定

```go
approveHTTP := rest.Bind("POST", "/:id/approve", approver, decodeApprove)
orders := host.Module("orders").Binding(approveHTTP)
```

业务公开 `OrderApprover.Approve(ctx, ApproveOrder)`，返回 `Approval`。领域规则是普通 Go，单资源动作复用 CRUD.Execute 的授权、事务与审计。HTTP 与直接 Go 调用使用同一具名业务对象；传输接口只需 Execute 和同源 AuthorizationBinding，不依赖 Command 类型。Module 从同一绑定取得目录与入口。

旧 `rest.Action` / ActionHandler 已移除。实际 Worker/CLI 可以调用同一业务方法，仍必须提供可信身份并参与既有入口核对；本候选不新增通用 Job 调度器、收据或效果解释器。多资源授权不自动创造跨数据库事务。

## 4. 显式 ES 模型

`domain/eventsourced.Model[S,E]` 由 Initial、Clone、Reduce 组成；Replay 和业务 Decide 不要求嵌入聚合基类或反射注册。`app/eventsourced.StateRepository` 从已有事件流加载状态，以隔离的基线副本执行决定，再重放产出的事件并使用原版本追加。

- Clone 必须真正复制 map、slice、pointer 等可变引用。失败后原基线保持；决定函数对临时状态的修改本身不会持久化，只有事件推进状态。
- 不自动重试业务决定。调用方版本过期返回冲突，事件未知、版本不连续、取消或归约失败均不得继续追加。
- 空事件不写入也不验证数据库最新版本，不可把空决定当作并发锁。
- 原事件适配器继续负责 codec、upcast、信封、顺序、版本、Direct/Outbox 发布。事件 wire type/schema/payload 不变。具体事件类型 E 的值与指针表示在回放时规范化，避免内存与 JSON 后端差异；仅同名但类型不同的事件仍拒绝。多事件接口 E 保留实际事件类型，Decide/Reduce 应遵循 registry 的指针事件约定。
- 显式路径不使用旧聚合快照，传入 SnapshotManager 时构造失败；它从完整事件重放，旧反射聚合及快照路径仍可使用。不能将不同状态形状写进同一旧快照格式。
- StateRepository 是持久化工具；业务注入 StateApplication，统一执行基线与事件归约后、追加前的授权/范围/保护字段检查。提交后失败仍为 AfterCommitError，不得当作未提交安全重试。

详见[授权执行迁移](next-authorization-migration.md)。代表性样例 `gochen-runtime/examples/domain/explicit-eventsourced` 与测试验证新旧路径读取同一事件流；并保留旧聚合便利 API，不强迫所有领域对象改为函数式模型。

### 事件历史命名与权限迁移

ES `Application.AuditTrail` 改为 `EventHistory`，`ActionAuditRead` 改为 `ActionEventHistoryRead`，动作名为 `event_history_read`。默认权限由 `<resource>:api:audit_read` 改为 `<resource>:api:event_history_read`。旧动作和方法无兼容别名，所有三仓内调用点同步迁移。

实际项目若手工挂载 ES 历史路由，应改为例如 `/:id/event-history` 并同步调用新的 EventHistory；本三仓原来没有内置 ES 历史 REST 路由，因此没有虚构一个自动路径迁移。业务希望暂时保留原权限码时，可在 ActionPermissions 中为新动作显式配置旧 permission，再按项目迁移权限数据；不自动重写或扩大任何既有用户授权。CRUD 的 audit_read 不变。

ES 事件历史不等同于操作审计，不能声称包含拒绝、失败请求或敏感读取记录。

## 5. 验收与迁移边界

- Core 测试：普通 Entity 可同事务审计且无需删除状态；仅删除状态可软删/恢复/永久删且无审计；失败审计、批量失败、维度隔离、Transfer、提交后失败继续回归。
- Runtime：SQL 测试覆盖独立字段组件、原列映射、事务与版本；REST 校验独立能力并核对实际授权来源；notes 与订单审批是完整业务切片。
- Contrib：GORM 与 Gin/nethttp 对照相同资源能力、认证/授权、版本及入口契约；实际 MySQL/PostgreSQL 由固定上游提交的数据库 CI 验证。
- 显式 ES：新旧重放一致、未知事件失败、引用隔离、陈旧写入、取消、AfterCommitError、原快照拒绝。

三仓分别 build/vet/test/race，Runtime 运行 examples smoke，Core 保持零外部依赖。最终执行结果以对应提交验收记录为准。

没有迁移 IAM、LLM、Workflow 或业务仓。下游需要同步替换旧包/API、按统一配置重建授权描述、显式选择 REST 路由，审查 ES 历史权限和手写路由，再运行自身测试。旧接入点盘点仍见 [核心迁移清单](core-next-migration.md)，不是下游已经兼容的证明。

仅推送三个 next，Core → Runtime → Contrib 顺序，后两仓锁定真实上游 SHA。没有合并 main、发布模块版本、改动持久数据或生产部署；dotless module path 与同级开发态 replace 保留。

### 生命周期验收边界

普通和软实体分别显式构造模式；缺能力/未配置模式失败关闭。回归包括重复软删不清除、硬模式清理旧软删行、一次 bulk 授权与事务回滚、purge-only 允许而 delete-only 拒绝、purge 钩子与审计一致、独立历史不丢失、恢复/查询独立开启、禁止通用 Execute 借用生命周期权限，以及只读应用无生命周期入口。三仓分别 build/vet/test/race，Core 保持零外部依赖。
