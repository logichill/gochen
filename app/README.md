# app：应用层模板

`app` 将领域对象、仓储与事件能力编排为可复用用例。依赖由组合根注入，HTTP 路由注册由 `gochen-runtime/api/rest` 承担。

## 模块

| 包 | 职责 |
| --- | --- |
| `app/crud` | CRUD、校验、查询分页、批量操作与 Hooks |
| `app/audited` | 审计轨迹、软删除、恢复与已删除查询 |
| `app/eventsourced` | 领域事件持久化、事件溯源仓储与命令服务 |
| `app/query` | Filter、QuerySchema、分页、排序与字段选择协议 |
| `app/operation` | 写操作结果、状态跟踪与 SSE |
| `app/security` | Action / Scoped 应用层保护 |

基础 `crud`、`audited`、`eventsourced` 模板不依赖 `auth`。安全策略通过 Application 装饰器装配，详见[分层授权](../docs/architecture/layered-authz.md)。

## CRUD 装配

`crud.NewApplication(repository, validator, config)` 返回 `(*Application[T, ID], error)`。仓储是必需依赖，validator 与 config 可为空；查询、批量等可选仓储能力在使用前探测。

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
	app, err := crud.NewApplication(repo, nil, nil)
	if err != nil {
		return nil, err
	}
	app.SetHooks(&crud.Hooks[*User, int64]{
		BeforeCreate: func(ctx context.Context, user *User) error {
			if strings.TrimSpace(user.Name) == "" {
				return errors.NewCode(errors.Validation, "user name is required")
			}
			return nil
		},
	})
	return app, nil
}
```

## Hooks 与事务

写入扩展统一通过 `SetHooks`、Runtime `rest.WithHooks` 或 builder 的 `Hooks` 注入。

| 阶段 | 执行位置 | 失败语义 |
| --- | --- | --- |
| `Before*` | 校验和写入之前 | 阻断后续写入 |
| `After*` | 仓储写入后、事务提交前 | 在事务边界内回滚 |
| `PostCommit*` | 事务提交之后 | 不回滚已提交写入 |

`PostCommit*` 要求仓储实现 `app/crud.ITransactional`。通知、外部同步等副作用应在提交后执行；需要回滚保证的 Hook 应使用事务仓储。

## 审计型 CRUD

`audited.NewApplication(repo, validator, config, auditStore)` 在构造期要求实体实现 `domain/audited.IAuditedEntity`，仓储支持事务、包含软删读取与已删除列表，auditStore 非空。

业务写与审计写必须同库同事务。调用方根据已认证身份通过 `contextx` 注入 operator；缺失时在持久化前失败。REST 审计端点还需配置 `RouteConfig.Audit.OperatorExtractor`。审计时间使用真实墙钟。

## 事件溯源与写操作协议

事件溯源按 `DomainEventStore → EventSourcedRepository → EventSourcedService` 装配，详见[事件溯源速查](../docs/reference/ddd-eventsourcing-quick-reference.md)。

普通写入可直接返回业务结果；需要统一信封或延迟收敛跟踪时使用 [Operation](operation/README.md)。多步骤补偿与流程推进见 [process](../process/README.md)。

完整 CRUD、审计与事件溯源示例位于 `gochen-runtime` 仓库 `examples/domain/`。
