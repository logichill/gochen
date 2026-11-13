# app：应用能力

`app` 将领域对象、仓储与事件能力编排为可复用用例。依赖由组合根注入，HTTP 路由注册由 `gochen-runtime/api/rest` 承担。

## 模块

| 包 | 职责 |
| --- | --- |
| `app/crud` | CRUD、校验、查询分页、批量操作与 Hooks |
| `app/crud/audit` | 独立的审计历史读取 |
| `app/eventsourced` | 事件溯源仓储、命令用例与历史查询；持久化适配位于 eventstore 子包，订阅辅助位于 subscriber 子包 |
| `app/query` | Filter、QuerySchema、分页、排序与字段选择协议 |
| `app/operation` | 写操作结果、状态跟踪；SSE 适配位于 Runtime |

`crud`、`audit`、`eventsourced` 通过 `Options.Authorizer` 接收 `auth.IAuthorizer`；授权决策中的 `spec.Spec` 交给仓储执行，详见[分层授权](../docs/architecture/layered-authz.md)。

## 授权与资源契约

读取仓储组合接口显式包含 `app.IExecutionDescriptorProvider`，由仓储本体暴露已绑定的只读资源契约。Application 构造继续校验实体类型、Vocabulary.Refs / Assignable 及审计数据源。全部维度默认受普通更新保护，mutable 分类支持业务编辑。授权条件采用类型化 Ref，Named 用于仓储业务读查询。

CRUD 的 `Transfer` 接收调用方版本和目标归属，分别授权转出与接收，再在同一事务中写入、回读实际状态并保存两侧审计。迁移钩子支持 After / PostCommit。字段配置和完整执行规则见 [资源契约](../docs/architecture/resource-contracts.md)。

单资源具名动作直接复用 `Execute`；多资源服务先预检全部资源，再在确有支持的同源事务中调用各受保护应用。应用仍正式授权，不使用命令专属 Grant 或上下文授权缓存。见[强类型授权接入](../docs/guides/declarative-authorization.md#多资源业务编排)。

## CRUD 与审计实现的文件组织

`crud/application.go` 定义统一资源应用和构造器，删除生命周期与普通写入共享 `app/internal/execution` 的读写、批量、生命周期和审计执行骨架。`crud/read_application.go` 定义只读应用，`read_application_read.go` 与 `application_write.go` 提供普通读写入口，`application_lifecycle.go` 承载删除、恢复、永久清除、回收站和冻结删除动作；`interfaces.go`、`options.go`、`hooks.go` 承载契约与配置。`crud/audit/reader.go` 独立定义历史查询接口、配置和构造器。

删除生命周期由统一 CRUD Application 构造并冻结；独立审计历史由组合根单独构造和授权，`app/crud` 不依赖 `app/crud/audit`。审计记录和存储契约仍在顶级 `gochen/audit`，执行骨架不迁出 `app/internal/execution`。

## CRUD 装配

`crud.NewApplication(repository, options)` 返回 `(*Application[T, ID], error)`。仓储须实现 `crud.Repository` 的读写、查询和事务端口；只读应用使用 `NewReadApplication` 和 `IReadRepository`，返回 `(*ReadApplication[T, ID], error)`；公开类型不提供 Create、Update、Delete、Execute 或 Transfer 等写入口。`Application` 与 `ReadApplication` 通过非导出的读取实现复用授权与查询语义，外部不能替换已绑定的执行核心。

`crud.Options[T, ID]` 以值传入，要求显式配置 `Resource`、`Authorizer` 与写应用的 `DeleteMode`，并在构造时冻结动作、Hooks、校验器和配额。闭包引用对象的并发安全由调用方保证。`IApplication` 组合读写、批量执行与元数据端口；`IReadApplication` 只要求资源查询，`IAuditReader[ID]` 独立表达审计读取。CRUD 的具体类型也不含 `AuditTrail`；`Options.Audit` 只配置同事务写入，历史读取通过独立 `app/crud/audit.Reader` 声明 `audit_read`。标准动作由构造器生成，自定义动作通过 `Options.Actions` 声明。

`List` / `Query` 按应用上限规范分页。`QueryCount` 与 `Query` 使用相同的 Filters/Advanced 和列表授权；计数忽略分页、排序和投影。`Count` 统计全部可见实体。

以下函数为已有仓储配置动作钩子：

```go
package example

import (
    "context"
    "strings"

    "gochen/app/crud"
    "gochen/auth"
    domcrud "gochen/domain/crud"
    "gochen/errors"
)

type User struct {
    domcrud.Entity[int64]
    Name string
}

func NewUsers(repo crud.Repository[*User, int64], authorizer auth.IAuthorizer) (*crud.Application[*User, int64], error) {
    return crud.NewApplication(repo, crud.Options[*User, int64]{
        Resource: "user",
        DeleteMode: crud.DeletePurge,
        Authorizer: authorizer,
        Hooks: crud.Hooks[*User]{
            crud.ActionCreate: crud.Stage[*User]{
                Before: func(ctx context.Context, user *User) error {
                    if strings.TrimSpace(user.Name) == "" {
                        return errors.NewCode(errors.Validation, "user name is required")
                    }
                    return nil
                },
            },
        },
    })
}
```

## Hooks 与事务

写入扩展统一通过构造参数 `Options.Hooks` 注入。REST 注册只配置传输行为，不修改应用；HTTP、Worker 和 CLI 调用同一应用时遵循同一组校验与钩子。

Hooks 以动作名为键，标准写动作使用 `create` / `update`，软删用 `delete`，物理清除用 `purge`，恢复用 `restore`，自定义动作使用其声明名称。单条和批量共用 `Stage[T]` 的 `Before`、`After`、`PostCommit`。`ExecuteBulk` / `ExecuteNewBulk` 一次授权、一个事务，提交前失败整批回滚。

| 阶段 | 执行位置 | 失败语义 |
| --- | --- | --- |
| `Before` | 命令产出目标状态之后、校验和写入之前 | 阻断后续写入 |
| `After` | 仓储写入后、事务提交前 | 在事务边界内回滚 |
| `PostCommit` | 事务提交之后 | 不回滚已提交写入 |

`PostCommit` 要求仓储实现 `app.ITransactionRunner`。通知、外部同步等副作用应在提交后执行；需要回滚保证的 Hook 应使用事务仓储。

版本由 条件仓储根据写入前版本统一推进；审计信息设置保持版本。单次 Update 写入失败时恢复进入时的对象版本。批量或外层事务回滚后，调用方应重新读取实体，再发起后续操作；数据库回滚与调用方内存对象的恢复分别遵循具体仓储契约。

`Before` 接收待写实体。审计 `Projector`、`After` 与 `PostCommit` 各自接收独立副本；展示或通知逻辑的输入修改与持久化实体、返回实体、审计维度事实相互隔离。无法安全复制的状态在消费前返回 `Unsupported`，支持的状态形状见 [实体状态复制](../domain/README.md#实体状态复制)。

`Execute` / `ExecuteNew` / 批量命令及 `Restore` 在提交后回调失败时返回已提交实体与 `contextx.AfterCommitError`。实体的 ID、版本与业务状态保持有效，调用方可据此返回降级成功或补偿失败的提交后阶段。

`Purge` 的业务校验应使用 `GetWithDeleted` 读取记录，避免已软删除的实体被普通 `Get` 遮蔽。`Purge` / `Restore` 只执行各自显式配置的 Stage；空 Stage 不执行钩子，不回退到 `Delete` / `Update`。

通用 `Read(action, fn)` 与四种 `Execute` 入口均在授权和回调前拒绝 delete / restore / purge / list_deleted / audit_read 等保留动作；普通 Get/Exists 固定使用 read，List/Query/QueryCount/Count 固定使用 list，只有专用 ListDeleted 使用 list_deleted。正常 list 与自定义列表动作不变。

普通 `Update` / `Execute` / `ExecuteBulk` 冻结加载态的 IsDeleted、DeletedAt 与可取得的 DeletedBy；输入、命令回调、Before 和校验器不得改变删除态；普通更新同时冻结真实加载的 ID、期望版本和资源保护字段，最后一次校验后再次核对，违反时 Forbidden，整批事务回滚。显式 mutable 维度仍遵循原资源契约。普通 Create / ExecuteNew / ExecuteNewBulk 必须产出未删除实体（删除标记和归因为空），Before 与校验后再次核对，不能以出生即删绕过生命周期权限。仅保护删除状态，不冻结其它托管时间戳或修改归因；Delete / Restore / Purge 使用专用状态迁移管线。

`Execute` / `ExecuteBulk` 先运行命令函数，再执行 `Before`，保证写前业务校验看到最终目标状态。更新按独立复制后的真实业务字段判断变化；字段 MarshalJSON 与审计 Projector 控制展示，隐藏字段的修改仍会持久化。无变化更新同样核对版本，合法 no-op 保持版本和审计记录数量。

`Restore` 在事务内装载已删除实体并清除删除标记，再执行 `Restore.Before`、实体验证与仓储写入；Before 后和最后一次实体验证后均核对真实加载的 ID、版本、资源维度和恢复态，禁止校验器重定向目标或重新软删；写入后执行 `Restore.After`，提交后执行 `Restore.PostCommit`。清除内存实体的删除标记不等于恢复已持久化；任一提交前步骤失败都会回滚事务。

## 独立审计与统一删除生命周期

`crud.Options.Audit` 接受 `app.IAuditRepository`（审计写入及数据源身份），不要求实体带操作者或软删除字段。构造器验证业务仓储和审计存储同源，业务与审计仍在同一事务中提交；审计失败回滚业务。`Projector` 继续只接收展示副本。

`app/crud/audit.NewReader[ID](queryStore, repository, options)` 独立绑定查询端口、资源有效元数据、`audit_read` 授权和分页。历史按记录自身的维度快照过滤，实体永久删除后仍可读。启用审计写入不会自动发布历史读取权限或路由。

`crud.Options.DeleteMode` 是写入应用必填配置：`crud.DeleteSoft` 使 Delete/DeleteBulk 只标记软删，重复调用保持 NotFound/Conflict，不会升级为物理清除；`crud.DeletePurge` 使它们进入完整 Purge 流程。零值或未知模式构造失败。类型实现 `ISoftDeletable` 不选择模式。构造后模式冻结，`DeleteAction()` 返回实际动作 delete 或 purge。

Purge 始终物理清除，包括旧软删行；硬模式的 Delete 使用 purge 授权、钩子和审计，描述不声明 delete。硬模式旧 delete 权限覆盖和钩子必须迁移为 purge，不能只更改权限码而保留错误动作。软模式同时声明 delete 和 purge。

`EnableRestore` 与 `EnableDeletedQuery` 分别选择恢复和回收站读取，可在任一删除模式下管理旧软删记录。未启用时即使仓储有对应方法，Descriptor 也不声明动作，入口拒绝执行。所有写应用要求真实 `IPurgeRepository`；软实体还要求独立 `IDeletedReadRepository`，物理清除不强制 Restore。只读应用没有任何生命周期写入口，也不能配置删除模式或生命周期选项。

删除能力不等于 HTTP 暴露。REST 默认普通 CRUD；恢复、回收站及显式永久清除路由须经 `rest.WithRoutes` 单独选择。`DELETE /:id` 与 bulk 使用 Application 的实际删除动作；`rest.WithAudit(history)` 继续独立组合历史读取。已移除独立 deletion 包与 WithDeletion，不保留别名或转发。

审计记录要求可信 operator；调用方从可信身份通过 contextx 注入，或配置 REST WithOperatorExtractor。独立修改归因字段沿用仓储的上下文 / DefaultActor 填充规则，默认仓储 actor 为 system；这不等于已认证用户。纯删除状态不要求 operator。`domain.ChangeInfo` 与 `domain.DeletionState` 可分别嵌入；`domain/crud.TrackedEntity` 只是保留原字段形状的便利组合，不是接入门槛。

## 事件溯源与写操作协议

显式 ES 使用 `domain/eventsourced.Model` 与 `StateApplication` 表达业务，固定执行授权 → 回放基线检查 → Decide → Replay 最终状态检查 → Append(expectedVersion)，不要求状态继承聚合基类。`StateRepository` 仍是低层提交工具，不作为普通业务授权入口。既有反射路径仍可按 `eventstore.DomainEventStore → EventSourcedRepository → Application` 装配，详见[事件溯源速查](../docs/reference/ddd-eventsourcing-quick-reference.md)。

普通写入可直接返回业务结果；需要统一信封或延迟收敛跟踪时使用 [Operation](operation/README.md)。多步骤补偿与流程推进见 [process](../process/README.md)。

完整 CRUD、审计与事件溯源示例位于 `gochen-runtime` 仓库 `examples/domain/`。

## 接入迁移

- 查询端口为 `IReadApplication` 的 Get/List/Query/Count/QueryCount，审计调用方显式依赖 `app/crud/audit.IAuditReader`。`NewReadApplication` 的返回类型改为 `*ReadApplication`，不再接受将只读结果赋给 `*Application` 或 `IApplication`；批量写操作使用完整应用的 `ExecuteBulk` / `ExecuteNewBulk` / `DeleteBulk`。
- 构造配置使用 `crud.Options`，业务批量与分页配额由 `Options.Limits` 设置。
- Purge / Restore 的业务校验与副作用配置在各自动作的 Stage。
- REST 使用 `Resource` / `Bind`；HTTP 请求体验证和批量限制分别使用 `WithValidator`、`WithMaxBulkSize`。
- 仓储读取和写入显式接收 `spec.Spec`，隔离和资源由构造选项 `WithIsolation` / `WithResourceKind` 声明。

## 公共配置与审计契约

共享配置在 `gochen/app`：各用例使用 `app.Limits`，非正上限默认归一为 1000。`app.Projector[T]` 只接收独立展示副本。应用登记依赖只含 Descriptor 的 `app.IDescriptorProvider`。

CRUD 仓储的事务能力为 `app.ITransactionRunner`。`app.IAuditRepository` 只组合审计写入和数据源标识；历史查询独立使用 `audit.IAuditQueryStore`。实现必须使用事务回调的上下文参与同一事务，仅返回相同的数据源字符串不足以保证原子性。

公开消费例子见 `options_test.go`，完整迁移见 [业务能力与入口绑定](../docs/guides/business-capabilities-next.md)。
