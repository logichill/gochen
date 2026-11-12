# app：应用层模板

`app` 将领域对象、仓储与事件能力编排为可复用用例。依赖由组合根注入，HTTP 路由注册由 `gochen-runtime/api/rest` 承担。

## 模块

| 包 | 职责 |
| --- | --- |
| `app/crud` | CRUD、校验、查询分页、批量操作与 Hooks |
| `app/audited` | 审计轨迹、软删除、恢复与已删除查询 |
| `app/eventsourced` | 事件溯源仓储、命令用例与历史查询；持久化适配位于 eventstore 子包，订阅辅助位于 subscriber 子包 |
| `app/query` | Filter、QuerySchema、分页、排序与字段选择协议 |
| `app/operation` | 写操作结果、状态跟踪；SSE 适配位于 Runtime |

`crud`、`audited`、`eventsourced` 通过 `Options.Authorizer` 接收 `auth.IAuthorizer`；授权决策中的 `spec.Spec` 交给仓储执行，详见[分层授权](../docs/architecture/layered-authz.md)。

## 授权与资源契约

读取仓储组合接口显式包含 `app.IExecutionDescriptorProvider`，由仓储本体暴露已绑定的只读资源契约。Application 构造继续校验实体类型、Vocabulary.Refs / Assignable 及审计数据源。全部维度默认受普通更新保护，mutable 分类支持业务编辑。授权条件采用类型化 Ref，Named 用于仓储业务读查询。

CRUD / Audited 的 `Transfer` 接收调用方版本和目标归属，分别授权转出与接收，再在同一事务中写入、回读实际状态并保存两侧审计。迁移钩子支持 After / PostCommit。字段配置和完整执行规则见 [资源契约](../docs/architecture/resource-contracts.md)。

通过 `Options.CommandGrant` 绑定到命令的应用在 `Transfer` 入口返回 `FailedPrecondition`。两阶段迁移使用独立应用；命令内的单资源判定复用范围见[强类型授权接入](../docs/guides/declarative-authorization.md#多资源命令)。

## CRUD 与审计实现的文件组织

`crud` 与 `audited` 在各自的 `application.go` 定义应用和构造器，共享 `app/internal/execution` 的读写、批量、生命周期和审计执行骨架。`crud/application_read.go` 与 `application_write.go` 提供公共入口；`interfaces.go`、`options.go`、`hooks.go` 承载契约与配置。

## CRUD 装配

`crud.NewApplication(repository, options)` 返回 `(*Application[T, ID], error)`。仓储须实现 `crud.Repository` 的读写、查询和事务端口；只读应用使用 `NewReadApplication` 和 `IReadRepository`。

`crud.Options[T, ID]` 以值传入，要求显式配置 `Resource` 与 `Authorizer`，并在构造时冻结动作、Hooks、校验器和配额。闭包引用对象的并发安全由调用方保证。`IApplication` 组合读写、批量执行与元数据端口；标准动作由构造器生成，自定义动作通过 `Options.Actions` 声明。

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

Hooks 以动作名为键，标准写动作使用 `create` / `update` / `delete`，审计应用另有 `purge` / `restore`，自定义动作使用其声明名称。单条和批量共用 `Stage[T]` 的 `Before`、`After`、`PostCommit`。`ExecuteBulk` / `ExecuteNewBulk` 一次授权、一个事务，提交前失败整批回滚。

| 阶段 | 执行位置 | 失败语义 |
| --- | --- | --- |
| `Before` | 命令产出目标状态之后、校验和写入之前 | 阻断后续写入 |
| `After` | 仓储写入后、事务提交前 | 在事务边界内回滚 |
| `PostCommit` | 事务提交之后 | 不回滚已提交写入 |

`PostCommit` 要求仓储实现 `app/crud.ITransactional`。通知、外部同步等副作用应在提交后执行；需要回滚保证的 Hook 应使用事务仓储。

版本由 CRUD/Audited 仓储根据写入前版本统一推进；审计信息设置保持版本。单次 Update 写入失败时恢复进入时的对象版本。批量或外层事务回滚后，调用方应重新读取实体，再发起后续操作；数据库回滚与调用方内存对象的恢复分别遵循具体仓储契约。

`Before` 接收待写实体。审计 `Projector`、`After` 与 `PostCommit` 各自接收独立副本；展示或通知逻辑的输入修改与持久化实体、返回实体、审计维度事实相互隔离。无法安全复制的状态在消费前返回 `Unsupported`，支持的状态形状见 [实体状态复制](../domain/README.md#实体状态复制)。

`Execute` / `ExecuteNew` / 批量命令及 `Restore` 在提交后回调失败时返回已提交实体与 `contextx.AfterCommitError`。实体的 ID、版本与业务状态保持有效，调用方可据此返回降级成功或补偿失败的提交后阶段。

资源专属命令通过 `CommandIdentityCall` 绑定全局 ID 服务能力。服务返回错误时，能力保留安全复制的部分结果和原错误（包含 AfterCommitError）；结果复制失败时清空输出并合并服务错误与复制错误。授权拒绝仍返回零结果，执行结束撤销能力。

聚合读取通过 `CommandReader` 接收授权条件；装备切换等专属原子写入通过 `CommandWriter` 接收条件及冻结 ID 意图。专属写入要求写动作、同一资源实例且没有权威 Assign，装配回调负责实际仓储的条件、归属、版本和事务保护；通用实体写入继续通过 `CommandRepository`。这些能力均在命令结束后撤销，专属写入失败时保留安全复制的部分结果。

`Purge` 的业务校验应使用 `GetWithDeleted` 读取记录，避免已软删除的实体被普通 `Get` 遮蔽。`Purge` / `Restore` 只执行各自显式配置的 Stage；空 Stage 不执行钩子，不回退到 `Delete` / `Update`。

`Execute` / `ExecuteBulk` 先运行命令函数，再执行 `Before`，保证写前业务校验看到最终目标状态。更新按独立复制后的真实业务字段判断变化；字段 MarshalJSON 与审计 Projector 控制展示，隐藏字段的修改仍会持久化。无变化更新同样核对版本，合法 no-op 保持版本和审计记录数量。

`Restore` 在事务内装载已删除实体并清除删除标记，再执行 `Restore.Before`、实体验证与仓储写入；写入后执行 `Restore.After`，提交后执行 `Restore.PostCommit`。清除内存实体的删除标记不等于恢复已持久化；任一提交前步骤失败都会回滚事务。

## 审计型 CRUD

`audited.NewApplication(repo, auditStore, options)` 接收 `audited.Options[T, ID]`（嵌入 `crud.Options`），实体须实现 `domain/audited.IAuditedEntity`，仓储须支持事务、恢复、已删除查询与永久删除。auditStore 必须提供写入、查询和数据源标识，与仓储同库同事务。应用直接提供 `Restore`、`Purge`、`ListDeleted`、`AuditTrail`。

调用方根据已认证身份通过 `contextx` 注入 operator；缺失时写入失败。REST 使用 `WithOperatorExtractor` 配置操作人，按 `Descriptor` 挂载标准动作。审计时间使用真实墙钟。审计查询限定当前应用资源与 ctx tenant，并按记录保存的边界应用规约，实体清洗后仍可读取。

## 事件溯源与写操作协议

事件溯源按 `eventstore.DomainEventStore → EventSourcedRepository → Application` 装配，详见[事件溯源速查](../docs/reference/ddd-eventsourcing-quick-reference.md)。

普通写入可直接返回业务结果；需要统一信封或延迟收敛跟踪时使用 [Operation](operation/README.md)。多步骤补偿与流程推进见 [process](../process/README.md)。

完整 CRUD、审计与事件溯源示例位于 `gochen-runtime` 仓库 `examples/domain/`。

## 接入迁移

- 查询端口统一为 `IReadApplication` 的 List/Query/Count/QueryCount；批量操作使用 `ExecuteBulk` / `ExecuteNewBulk` / `DeleteBulk`。
- 构造配置使用 `crud.Options` / `audited.Options`，业务批量与分页配额由 `Options.Limits` 设置。
- Purge / Restore 的业务校验与副作用配置在各自动作的 Stage。
- REST 使用 `Mount` / `Action`；HTTP 请求体验证和批量限制分别使用 `WithValidator`、`WithMaxBulkSize`。
- 仓储读取和写入显式接收 `spec.Spec`，隔离和资源由构造选项 `WithIsolation` / `WithResourceKind` 声明。
