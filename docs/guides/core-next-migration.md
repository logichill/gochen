# 核心三仓 next 候选的源码迁移

本轮只形成可审阅候选，不发布版本、不合并 main、不迁移业务仓和数据库。依赖方向仍为 Contrib → Runtime → Core；Core 仍无第三方依赖。

## 公开入口变化

| 原入口/写法 | 候选入口 | 核心三仓内消费者 |
| --- | --- | --- |
| 公共 Options 暴露 execution.Limits | `app.Limits` | CRUD、deletion、ES 与外部配置示例 |
| CRUD 仓储嵌入 internal 事务端口 | `app.ITransactionRunner` | CRUD、deletion、Runtime Repo、Contrib GORM 契约测试 |
| audited Projector 使用 internal 类型 | `app.Projector[T]` | audited 及共享执行实现 |
| 审计只声明写接口、构造时另断言读接口 | `app.IAuditRepository`，只组合写入及数据源身份；独立查询使用 `audit.IAuditQueryStore` | CRUD 与 deletion 可选审计；业务仓储同源检查保留 |
| `host.IDescriptorProvider` | `app.IDescriptorProvider` | Module、应用目录、REST |
| `RouteRegistrar(func() *Routes)` | `RouteRegistrar(routes)` 或 `module.RouteRegistrarFunc(fn)` | Host、HTTP 共同契约测试、示例 |
| `EventHandler(func() *Handler)` | `EventHandler(handler)` | typed Module 与冻结订阅清单 |
| `Projection(factory)` | `Projection(module.ProjectionOf[ID](projection))` | 投影注册/回滚 |
| `RuntimeComponent(factory)` | `RuntimeComponent(workerPointer)` | Worker 所有权、启动/关闭、入口核对 |
| `host/module/assembly` 公共转换结构 | `host.Module(...).Build()` 或自定义 `module.IModule` | 转换实现内收至 `host/internal/assembly` |
| `host.NewRegistry` / `host.Registry` | Host 管理实例；高级模块用 `module.ApplicationRegistryFrom(opts)` | REST `WithStartupRegistry` 接收 `module.IApplicationRegistry`；不再手工创建第二份目录 |
| quick 自有轻量模块实现 | quick 转换为正式 host.Module | Group/Mount/依赖/中间件语义保留 |

不保留旧入口的别名或双轨兼容层。普通装配在 ModuleCtor 中显式创建依赖一次，再登记同一个实例。`Provide` 仍可登记高级 DI 服务，但不会推测该服务的角色；依赖排序不等于服务定位。

## 正确性和资源边界

- Module Build 复制框架声明集合，typed nil 在构造期拒绝；函数路由须用显式转换。
- 非 HTTP 应用也参与跨应用动作/权限冲突检查；实际路由与后台入口仍由独立来源核对，不能用应用目录替代。
- 同类型不同实例可同时登记。同一 Worker 指针在整个 Host 内只能有一个生命周期所有者，非指针 Worker 必须包装为明确指针；高级 `RuntimeComponents` 暴露范围同样检查。
- 同一个对象同时担任 handler 和 worker 时，订阅 token 与生命周期分别管理。停止失败保留清理责任，不能将残留资源标成已清理。
- Build 不启动 goroutine。入口收集、校验、激活顺序不变。投影 ID 类型仍在真实注册时检查，失败完整回滚。
- 外部注入 DB/client 的关闭责任仍属于组合根，不因注册到 Module 而转移。

## 数据与执行语义不变

没有表/列、ID 编码、审计字段、事件 type/schema/payload、snapshot/checkpoint/outbox 的迁移。ES 事件历史动作由 audit_read 改为 event_history_read，默认权限同步变化；部署端必须显式迁移原 ES 历史授权，不修改 CRUD 审计权限。详见[能力拆分迁移](business-capabilities-next.md)。

CRUD 与 deletion 共用私有执行骨架；业务审计仍在同一个事务回调中写入，失败回滚，提交后失败仍为 `AfterCommitError`。审计查询使用历史 Dimensions 快照。ES 继续使用独立授权、回放、重试和事件保存流程；原子 SaveAll 及快照装饰器边界不变。Transfer、条件下推、租户与受保护维度合同保留。原多资源 Command API 已移除，改为显式业务预检和受保护应用编排，正式判定仍逐资源执行；同源事务边界需实际验证，不能自动声称跨存储原子提交。详见[授权执行迁移](next-authorization-migration.md)。

## 下游影响清单

本轮不修改 gochen-iam、gochen-llm、gochen-workflow、alife、ems、erp。它们切换候选前应只读检索上述旧符号、角色构造器以及自定义审计/事务实现。直接引用旧 assembly/Registry、传入角色工厂、使用值类型 Worker、审计存储缺数据源接口的消费者需要迁移。未使用这些入口的调用方也必须用匹配的三个仓库重新编译和跑契约测试。

本次只读读取三个下游 main 固定提交的全部 Go 文件（IAM 255、LLM 94、Workflow 34，共 383 文件），确认以下具体接入点：

- [IAM module.go:96–109](https://github.com/logichill/gochen-iam/blob/43941009b58c5ecee20615cce6322ee7e4bc96a5/module.go#L96-L109)：AuthRoutes、User/Role/Group/Tenant/Scope/Menu Routes 和 authConfigValidator 都通过旧工厂登记。
- [LLM module.go:43–50](https://github.com/logichill/gochen-llm/blob/942f53c370c1a159c6b0c9e7ba91f4e938b78f9f/module.go#L43-L50)：路由和 ProviderManager/ChatService Worker 仍传工厂；两个测试 [module_chat_entries_test.go](https://github.com/logichill/gochen-llm/blob/942f53c370c1a159c6b0c9e7ba91f4e938b78f9f/module_chat_entries_test.go#L100-L106)、[module_provider_entries_test.go](https://github.com/logichill/gochen-llm/blob/942f53c370c1a159c6b0c9e7ba91f4e938b78f9f/module_provider_entries_test.go#L77-L87) 直接引用旧 assembly。
- [Workflow module.go:23](https://github.com/logichill/gochen-workflow/blob/56d6051d720887deb8c1d1950702bab440f72670/module.go#L23)：RouteRegistrar 传入 httpapi.NewRegistrar 工厂。

上述 Go 文件未发现旧 host.Registry/IDescriptorProvider、internal execution 配置、EventHandler/Projection 或明确 audited 接入引用；这不证明所有自定义审计实现都满足新接口。alife、ems、erp 的仓库读取返回 404，未访问，不推断其状态。没有编译或运行任何下游测试，也未盘点非 Go 文档引用。待用户决定切换范围后，另做适配和验证。

## 候选检出与验证

保持三个 repo 名、module path、目录名一致，平级检出同一批 next 候选。开发态仍使用仓库原有的同级 replace，不制造不存在的发布版本。Runtime 和 Contrib 的候选说明记录上游真实 commit；审阅时锁定这些 commit，避免只追踪会前移的分支名。

分别在三个仓库运行 `GOWORK=off go build ./...`、`go vet ./...`、`go test -count=1 ./...`。Core `GOWORK=off go list -m all` 只能输出 gochen。Runtime 普通模块完整例子为 `examples/typed-module`；Contrib 的 `TestTypedModuleGORMHTTPContract` 对同一个 GORM 接入分别验证 net/http 和 Gin。

MySQL/PostgreSQL 外部数据库契约需要独立测试 DSN，不以 SQLite 成功替代。发布真实模块版本、移除本地 replace、改业务依赖和合并 main 是后续独立决定。
