# next：业务能力与入口绑定

本轮在既有 typed Module 候选上继续演进，优先让调用者看到实际业务能力，而不是再复制一套框架。CQRS 中间态暂缓。本文件明确本批边界；它不表示五点架构方向都已完成。

## 1. 本批公开形状

### 真正只读

`crud.NewReadApplication` 返回独立的 `*crud.ReadApplication[T, ID]`，不再返回包含写入、动作执行和归属迁移方法的 `*crud.Application`。`crud.IReadApplication` / `crud.IApplication` 不再强制包含 `AuditTrail`，审计读取通过 `crud.IAuditReader` 表达。

这次收窄的是公开职责，不是删除审计。已配置的 Audit 及 `audit_read` 权限、范围、历史过滤继续有效；具体只读类型仍保留可选审计读取。完整独立的审计构造和删除生命周期尚未实现，不能据此声称所有可选方法都从具体类型中消失了。

REST 按应用 Descriptor 挂载实际能力，构造/装配时拒绝缺失的声明能力，而不是悄悄丢弃已声明的审计或写入路由。普通无审计的只读消费者不再需要假装实现审计接口。

### Module 只声明一次资源

```go
host.Module("notes").
    Binding(rest.Resource(application)).
    Build()
```

`rest.Resource` 使用同一个应用对象贡献应用清单及标准路由。`module.IBinding` 返回明确的模块贡献，Host 仍负责统一校验、登记和生命周期。Binding 不创建数据库、不执行业务、不隐式启动任务，也不通过 `any` 猜测角色。

需要精细装配时仍可使用 `Application` / `RouteRegistrar` 等原有明确角色。它们是不同粒度的显式组合入口，不是另一套运行时。

### 一个命令，多种调用入口

```go
approveHTTP := rest.Bind("POST", "/:id/approve", approveCommand, decodeApprove)
orders := host.Module("orders").Binding(approveHTTP)
```

`rest.Bind` 接收已有的 `app.Command[Input, Access, Output]`。HTTP 只解码输入并编码输出，不能再提供一套与声明动作独立的业务 handler。动作身份、全部资源保护要求和执行函数都来自同一个命令；它沿用既有授权能力，不增加消息总线或计划解释器。

对业务调用方，公开的是 `OrderApprover.Approve(ctx, ApproveOrder)` 和 `Approval`，不是 action 字符串、grant 数组或修改实体的闭包。组合根可以理解 Command 的资源装配；领域规则和用例顺序不必理解这些机制。

原 `rest.Action` 与 `ActionHandler` 被移除。自定义入口迁移到命令绑定；不要仅把旧 handler 包一层、却继续声明另一个动作。使用 Audited 的旧示例保留原有应用写入协议，不为了迁移 HTTP 演示而换成绕开审计的裸仓储写入。

## 2. 业务验收入口

- Runtime `examples/typed-module`：普通 notes CRUD，只有 ID、Version 与业务字段；新 Resource binding、不安全演示身份入口的明确警告、真实 HTTP、版本冲突、Host 取消及外部资源所有权。
- Runtime `examples/business-actions`：命名订单审批、类型化输入和结果、独立领域规则、授权后的受约束仓储、真实内存 SQLite、HTTP 与普通 Go 调用同一命令。后台调用不等于自动拥有 HTTP 身份，必须明确提供可信主体。
- Contrib `http/gin/typed_module_test.go`：相同 GORM 数据库能力分别经 Gin 和 net/http，验证 Resource binding、只读资源、401/403、版本冲突及真实入口清单。

这是应用 API 与业务切片的验证，不是对操作回执和 Outbox 原子链的实现承诺。一个命令授权多资源也不意味着自动具有共同数据库事务；实际事务仍由用例明确选择。

## 3. 五点方向的本批落点与后续边界

| 方向 | 本批 | 后续必须先固定的契约 |
| --- | --- | --- |
| Audit 与删除生命周期独立 | 保留现有事务语义，不删除 Audited | 审计记录/端口归属、删除策略、构造与路由、下游同步迁移 |
| 接口表达真实能力 | 独立只读类型、审计读取窄接口、REST 装配校验 | 审计读取独立构造、可选删除生命周期类型 |
| 命名业务用例 | 订单审批纵向样板，领域规则和用例编排分开 | 用真实下游验证更复杂的多资源事务 |
| 动作声明一次 | Command 到 HTTP 的类型化绑定、Module 贡献 | 实际消息/Job 适配器需要时沿用同一命令；不先建通用调度层 |
| 显式 ES 业务主线 | 不修改 ES 存储、事件或权限 | reducer 与既有聚合仓储桥接、快照兼容、EventHistory 动作/路由/权限同步迁移 |

取消 Audited 模板不是简单改名，纯状态 ES 也不是给现有反射类型加一个别名。这两项应先形成可运行切片及完整迁移清单，再替换。当前没有引入长期并行的新 Audit/ES 框架，也没有改动现有表、事件 wire type/schema、历史权限码。

## 4. 下游迁移提示

- 显式接收 `*crud.Application` 的只读调用方改为 `*crud.ReadApplication` 或 `crud.IReadApplication`。不要依靠类型断言重新取写能力。
- 只依赖 `IReadApplication` / `IApplication` 却调用 `AuditTrail` 的地方，另行接收 `crud.IAuditReader`。审计需求应在构造处明确。
- Runtime 普通资源优先用 `Binding(rest.Resource(...))`，不要同时手动重复登记同一应用与路由。
- 自定义 `rest.Action` 改用 `rest.Bind`。原业务规则、事务、审计与授权必须保留，禁止只为了通过新接口而删除保护。
- 本次未迁移 IAM、LLM、Workflow 或业务仓库。上一批的固定源码接入点清单仍见 [核心迁移说明](core-next-migration.md)，本批新增接口影响不能当作下游已经通过编译。

## 5. 验证与发布边界

各仓独立执行 `GOWORK=off go build ./...`、`go vet ./...`、`go test -count=1 ./...`，Core 继续零第三方依赖；按改动复跑 race 和 Runtime examples smoke。最终实际通过/未运行结果以交付记录为准，不把本节命令清单当成已完成结果。

三个 next 分支按 Core → Runtime → Contrib 顺序发布，后两仓的 NEXT.md 和数据库契约 CI 固定真实上游提交。本批没有发布模块版本、没有 main 合并、没有数据库迁移或生产部署。开发态 dotless module path 与同级 replace 保持原样。
