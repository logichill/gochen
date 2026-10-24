# app：应用层模板

`app` 将领域对象、仓储与事件能力编排为可复用用例。依赖由组合根注入，HTTP 路由注册由 `gochen-runtime/api/rest` 承担。

## 模块

| 包 | 职责 |
| --- | --- |
| `app/crud` | CRUD、校验、查询分页、批量操作与 Hooks |
| `app/audited` | 审计轨迹、软删除、恢复与已删除查询 |
| `app/eventsourced` | 事件溯源仓储与命令服务；默认持久化位于 eventstore 子包，事件订阅辅助位于 subscriber 子包 |
| `app/query` | Filter、QuerySchema、分页、排序与字段选择协议 |
| `app/operation` | 写操作结果、状态跟踪；SSE 适配位于 Runtime |
| `app/security` | Action / Scoped 应用层保护 |

基础 `crud`、`audited`、`eventsourced` 模板不依赖 `auth`。安全策略通过 Application 装饰器装配，详见[分层授权](../docs/architecture/layered-authz.md)。

## CRUD 与审计实现的文件组织

`crud`、`audited` 包以 `application.go` 定义应用类型和构造过程，同一类型的方法按职责放在 `application_*.go` 中：

| 文件 | 职责 |
| --- | --- |
| `application_read.go` | CRUD 读取、查询与分页 |
| `application_write.go` | 单条写入与校验 |
| `application_batch.go` | 批量写入及其专用辅助逻辑 |
| `application_audit.go` | 审计存储访问、快照与投影 |
| `application_lifecycle.go` | 软删除、恢复、清除与已删除查询 |

这些文件共同实现包内的 `Application`，文件后缀用于定位职责。接口、配置与独立算法继续按自身职责命名，例如 `interfaces.go`、`capabilities.go`、`config.go`、`audit_diff.go`；写入流程装配与通用钩子定义位于 `crud` 包的 `flows.go`、`hooks.go`。

两种 Application 均直接实现 `crud.IBatchWriter`；审计应用使用自己的批量实现，以保留审计、软删除和事务语义。批量接口独立于 `crud.IApplication`，安全装饰器与传输层仍通过可选接口探测批量能力。

## CRUD 装配

`crud.NewApplication(repository, options)` 返回 `(*Application[T, ID], error)`。仓储是必需依赖，options 为 nil 时使用默认配置；查询、批量等可选仓储能力在使用前探测。

`crud.Options[T, ID]` 在构造期注入 Config、Validator 和 Hooks。Config 和 Hooks 被复制，构造后不会随调用方修改而变化；Validator 与钩子闭包引用的对象仍由调用方保证并发安全。`IApplication` 只包含读写用例，批量和审计属于可选业务能力。

以下函数展示给已有仓储配置 Hook 的完整写法：

```go
package example

import (
	"context"
	"strings"

	"gochen/app/crud"
	domcrud "gochen/domain/crud"
	"gochen/errors"
)

type User struct {
	domcrud.Entity[int64]
	Name string
}

func NewUsers(repo domcrud.IRepository[*User, int64]) (*crud.Application[*User, int64], error) {
	return crud.NewApplication(repo, &crud.Options[*User, int64]{
		Hooks: &crud.Hooks[*User, int64]{
			Create: crud.Stage[*User]{
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

钩子按写动作分为三组：`Create`（针对实体新增）、`Update`（针对实体更新）、`Delete`（针对主键删除）。每组提供三段式生命周期（`Before`、`After`、`PostCommit`），单条与批量操作共用，签名直接接收载荷，无需分支判断操作类型。

| 阶段 | 执行位置 | 失败语义 |
| --- | --- | --- |
| `Before` | 校验和写入之前 | 阻断后续写入 |
| `After` | 仓储写入后、事务提交前 | 在事务边界内回滚 |
| `PostCommit` | 事务提交之后 | 不回滚已提交写入 |

`PostCommit` 要求仓储实现 `app/crud.ITransactional`。通知、外部同步等副作用应在提交后执行；需要回滚保证的 Hook 应使用事务仓储。

## 审计型 CRUD

`audited.NewApplication(repo, auditStore, options)` 复用 `crud.Options[T, ID]`，在构造期要求实体实现 `domain/audited.IAuditedEntity`，仓储支持事务、包含软删读取与已删除列表，auditStore 非空。`IAuditOperations.HasPurgeCapability()` 报告硬删除能力，REST 据此注册 purge 路由，无需访问仓储。

业务写与审计写必须同库同事务。调用方根据已认证身份通过 `contextx` 注入 operator；缺失时在持久化前失败。REST 审计端点还需配置 `RouteConfig.Audit.OperatorExtractor`。审计时间使用真实墙钟。

## 事件溯源与写操作协议

事件溯源按 `eventstore.DomainEventStore → EventSourcedRepository → EventSourcedService` 装配，详见[事件溯源速查](../docs/reference/ddd-eventsourcing-quick-reference.md)。

普通写入可直接返回业务结果；需要统一信封或延迟收敛跟踪时使用 [Operation](operation/README.md)。多步骤补偿与流程推进见 [process](../process/README.md)。

完整 CRUD、审计与事件溯源示例位于 `gochen-runtime` 仓库 `examples/domain/`。

## 接入迁移

- `crud.NewBatchWriter(app)`、`audited.NewBatchWriter(app)` 及对应的 `BatchWriter` 类型已移除。直接调用 `app.CreateAll`、`app.UpdateAll`、`app.DeleteAll`；仅需批量能力时依赖 `crud.IBatchWriter`。
- 原构造参数 validator/config 与后续的 `SetHooks`、`SetValidator`、`UpdateConfig` 合并到 `crud.Options`；需要另一套业务行为时构造另一个应用。
- REST 的 `Service`、`Hooks`、`WithHooks`、`WithValidator` 已移除。应用策略在构造期注入；HTTP 请求体验证继续使用 `WithCustomValidator`。
- `rest.WithBatchOperations(n)` 仅限制该组 HTTP 批量请求。业务批量上限通过 `Options.Config.MaxBatchSize` 设置，两层限制独立生效。
- 自定义 L3 应用在组合根提供 `IRepositoryProvider`，可通过 `IConfigProvider` 提供授权前批量上限；未提供配置时使用默认 1000。先装配 scoped，再装配 action，或使用 Runtime `security.Profile.Build`。安全外壳不转发仓储、配置或独立 Validate 方法。
