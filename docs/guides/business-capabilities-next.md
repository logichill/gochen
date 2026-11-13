# next：业务能力、独立审计与显式 ES

本候选完成核心三仓内的业务能力拆分，不再把 CRUD、Audited、ES 当作三档应用模板。CRUD 是资源操作，Audit 是可选事务记录与独立历史读取，删除是独立生命周期，ES 是权威事件及恢复路径。CQRS 中间态继续暂缓。

## 1. 资源、审计与删除分别构造

- `crud.NewReadApplication` 返回真正只读的 `*crud.ReadApplication`，没有写入、执行、Transfer 或 AuditTrail 方法。
- `crud.NewApplication` 是唯一通用 CRUD 门面。`Options.Audit` 只选择审计写入，不强制历史查询、软删除或操作者字段。
- `app/audit.NewReader[ID](queryStore, repository, Options{...})` 只提供审计历史。资源元数据来自 repository 的有效契约，查询按历史 DimensionSnapshot 授权，永久删除之后仍可读取。读取动作仍是 `audit_read`。
- `deletion.NewApplication(repository, deletion.Options{Options: crud.Options{...}})` 只提供 Delete/DeleteBulk/Restore/Purge/ListDeleted。它不复制 CRUD 的读写和 Execute/Transfer。需要事务审计时才配置 Audit。
- `audit.IAuditStore` / `audit.IAuditQueryStore` 分别表达记录写入与读取。写入端 `app.IAuditRepository` 只再要求数据源身份，同源校验与实际事务参与都不可省略。

旧 `app/audited`、`domain/audited.IAuditedEntity`、`IAuditedRepository` 已移除，不保留兼容别名。领域可分别使用 `domain.ChangeInfo`（修改归因）、`domain.DeletionState`（删除状态）。`domain/crud.TrackedEntity` 仅是旧字段形状的便捷组合，普通 Entity 也能启用完整审计。

软删除实体的 CRUD 描述中没有 delete，直接调用 CRUD.Delete 也不能物理删除它。软删只走 deletion.Delete，永久删除只走 Purge。物理仓储仍遵循已有条件写入、版本与事务契约。没有将历史字段、表名或 JSON 层级重新解释为新结构。

## 2. REST 与 Module 显式组合

```go
host.Module("notes").
    Binding(rest.Resource(resources,
        rest.WithAudit[*Note, int64](history),
        rest.WithDeletion[*Note, int64](trash))).
    Build()
```

不需要审计或删除的模块仍只有 `Binding(rest.Resource(resources))`。每个能力贡献自己的动作、权限、执行对象；HTTP 不再通过最大接口猜测横切能力。不同资源混装、重复动作、nil 能力在装配时失败。各动作使用实际能力的 AuthorizationBinding，不能以普通 read 的授权替代 audit_read。

CRUD 审计路由仍是 `GET /:id/audit`；删除路由仍是 `DELETE /:id`、`DELETE /bulk`、`POST /:id/restore`、`DELETE /:id/purge` 和 `GET /deleted`。审计记录要求可信 operator；独立修改归因字段沿用仓储的上下文 / DefaultActor 填充规则（默认 system，不代表用户认证）。纯删除状态不要求操作者或 HTTP extractor。需要时使用已有 WithOperatorExtractor，或由认证边界写入 contextx。

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

- Core 测试：普通 Entity 可同事务审计且不带删除；仅删除状态可软删/恢复/永久删且无审计；失败审计、批量失败、维度隔离、Transfer、提交后失败继续回归。
- Runtime：SQL 测试覆盖独立字段组件、原列映射、事务与版本；REST 校验独立能力并核对实际授权来源；notes 与订单审批是完整业务切片。
- Contrib：GORM 与 Gin/nethttp 对照相同资源能力、认证/授权、版本及入口契约；实际 MySQL/PostgreSQL 由固定上游提交的数据库 CI 验证。
- 显式 ES：新旧重放一致、未知事件失败、引用隔离、陈旧写入、取消、AfterCommitError、原快照拒绝。

三仓分别 build/vet/test/race，Runtime 运行 examples smoke，Core 保持零外部依赖。最终执行结果以对应提交验收记录为准。

没有迁移 IAM、LLM、Workflow 或业务仓。下游需要同步替换旧包/API、拆分授权描述、显式添加 REST 能力，审查 ES 历史权限和手写路由，再运行自身测试。旧接入点盘点仍见 [核心迁移清单](core-next-migration.md)，不是下游已经兼容的证明。

仅推送三个 next，Core → Runtime → Contrib 顺序，后两仓锁定真实上游 SHA。没有合并 main、发布模块版本、改动持久数据或生产部署；dotless module path 与同级开发态 replace 保留。
