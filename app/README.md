# app/：应用层模板（Application Layer）

`app/` 位于 gochen 分层架构的“应用层”：**把领域对象与基础设施能力编排成可复用的用例模板**，并为组合根提供“少样板、可替换”的装配入口。

## 1. 在分层中的位置

```
domain/（领域抽象与规则）
   ↑ 依赖接口
app/（用例编排/模板）
   ↑ 依赖实现
eventing/、messaging/、db/、cache/、httpx/（基础设施）
```

关键边界：

- `app/` 可以依赖 `domain/` 与基础设施模块；
- `domain/` 不反向依赖 `app/` 与基础设施；
- “创建哪些实现、注入哪些依赖”由业务组合根决定（不要在库层隐式创建）。

## 2. 子模块导航（你该看哪个）

| 子模块             | 解决的问题                                      | 推荐入口                                  |
| ------------------ | ----------------------------------------------- | ----------------------------------------- |
| `app/crud`         | 通用 CRUD 应用服务（校验/查询分页/批量/hooks）  | `crud.NewApplication`                     |
| `app/audited`      | CRUD + 软删/审计/恢复能力组合                   | `audited.NewApplication`                  |
| `app/eventsourced` | 事件溯源应用模板（DomainEventStore/History 等） | 示例优先：`examples/domain/eventsourced*` |

HTTP API 构建器（接口适配层）已从 `app/api` 迁移到根级 `api/rest`，见：`api/rest/README.md`。

## 3. 典型装配链路（保持链路不断）

> 业务侧常见约束是：`application -> service -> repo -> entity` 链路不能断。gochen 的模板也按此路径组织。

### 3.1 CRUD（最小闭环）

```
crud.IRepository[T,int64]
   -> crud.IApplication[T,int64]  （用例模板）
      -> rest.Register(...)     （HTTP 路由）
```

### 3.2 audited CRUD（额外依赖与约束）

```
audited entity（实现 audited.IAuditedEntity[int64]）
   + auditStore（audited.IAuditStore，必需）
   + repo 支持事务运行器（app/crud.ITransactional，必需，通过 `WithinTx` 保证业务写+审计写同事务提交）
   + operator（由 API 从请求中提取并注入 ctx，必需）
      -> audited.NewApplication(...)
         -> rest.Register(...) 自动启用 audited 端点（fail-fast 校验）
```

### 3.3 事件溯源（核心领域）

```
domain/eventsourced 聚合（聚合根 + 领域事件）
   -> app/eventsourced.DomainEventStore（依赖 eventing/store + snapshot + outbox + bus）
      -> app/eventsourced.EventSourcedRepository（默认仓储实现）
         -> app/eventsourced.EventSourcedService（命令执行模板）
         -> projection/outbox/subscription（读模型与可靠发布）
```

## 4. 最小示例（只展示“入口与约束”）

### 4.1 CRUD application（创建应用服务）

```go
repo := buildRepo() // crud.IRepository[*User,int64]
validator := buildValidator()

app, err := crud.NewApplication(repo, validator, nil)
if err != nil {
	// fail-fast：repository 为空等装配错误
	return err
}
_ = app // 交给上层（例如 gochen/api/rest 的 rest.Register）做路由注册
```

### 4.2 CRUD Hooks（推荐扩展点）

CRUD 写入扩展统一通过 `Hooks` 显式注入，不通过嵌入 `Application` 后覆写 `BeforeCreate` / `AfterCreate` 等方法。

```go
app, err := crud.NewApplication(userRepo, validator, nil)
if err != nil {
	return err
}
app.SetHooks(&crud.Hooks[*User, int64]{
	BeforeCreate: func(ctx context.Context, user *User) error {
		if strings.TrimSpace(user.Name) == "" {
			return errors.NewCode(errors.Validation, "user name is required")
		}
		return nil
	},
})
```

阶段语义固定为：`Before*` 写入前执行，失败会阻断写入；`After*` 写入后、事务提交前执行，失败会回滚；`PostCommit*` 事务提交后执行，失败不回滚已提交写入。未配置的 hook 为 no-op。

### 4.3 audited application（必须提供 auditStore）

```go
repo := buildRepo()        // crud.IRepository[*User,int64]，且实现 app/crud.ITransactional（WithinTx）
auditStore := buildStore() // audited.IAuditStore（必需）

auditedApp, err := audited.NewApplication(repo, validator, nil, auditStore)
if err != nil {
	// fail-fast：类型不是 audited / auditStore 为空等
}
_ = auditedApp
```

> 路由层会要求配置 `RouteConfig.Audit.OperatorExtractor`（写操作必须有 operator），详见 `api/rest/README.md`。

## 5. 进一步阅读

- REST CRUD 路由注册：`api/rest/README.md`
- 生命周期与组合根装配：`host/README.md`
- 整体边界与装配说明：`docs/architecture/framework-design.md`
- 下游项目接入与治理指南：`docs/guides/downstream-guide.md`
- 事件溯源快速参考：`docs/reference/ddd-eventsourcing-quick-reference.md`
- 事件溯源示例：`examples/domain/eventsourced`、`examples/domain/eventsourced_stringid`
