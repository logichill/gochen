# 强类型授权接入

资源、动作、权限码及范围规则由 Go 声明连接，组合根调用 `auth.Compile` 冻结计划并注入执行入口。声明随着代码构建和部署；部署配置继续控制认证开关及物理驱动，策略动态热更新沿用独立设计边界。

可运行入口：

- [纯 Core 示例](../../examples/auth/declaration/main.go)：声明编译、Application 读写范围与独立命令注册源核对。
- Runtime 的 `examples/auth/authorization`：可信权限及 DataScope 装配。
- Runtime 的 `examples/quick`：Quick Application 与 HTTP 装配。

## Application

1. 从仓储取得 `ExecutionDescriptor().Metadata()`，准备 Application Options。
2. 使用 `crud.Describe`、`deletion.Describe` 或 `eventsourced.Describe` 取得与实际执行同源的 Descriptor。
3. 在 `auth.Declaration` 中绑定 Descriptor、`ResourceAccessData` 和规则。
4. `auth.Compile` 接收权限检查器、实体元数据及可选治理配置；将生成的授权器注入 Application。

Runtime Quick 的 `DeclaredApp` 完成基础 CRUD 声明装配；删除生命周期与事件溯源 Application 使用各自的 Describe、Compile 与构造入口。具体签名与约束见各函数 GoDoc。

生产资源入口优先使用 `Compile`。`New` 提供低层策略与治理装配，其 `Plan()` 返回未绑定状态；Application 构造仍校验资源契约及词汇，`ValidatePlanBinding` 对未绑定计划直接返回。资源执行入口的 `BindExecution` 和启动门禁要求有效编译计划，因此 `New` 的构造成功仅证明低层执行内核完成装配。

Application 恒定访问资源数据。构造期核对动作权限、Kind、Transfer 信息及维度，验证词汇与有效资源契约。执行时自动授权，Repository 接收显式条件并继续执行隔离、版本和普通更新保护。列表、计数和按 ID 操作共享范围约束。

## 自定义授权包装契约

优先通过 `CompileOptions.Governance` 配置内置治理。围绕已编译 `Authorizer` 自定义透明包装时，包装类型须完整提供以下方法：

- `Authorize(context.Context, auth.Request) (auth.Decision, error)` 和 `Vocabulary() auth.Vocabulary`：保留正式判定和词汇副本。
- `Plan() (auth.Plan, bool)`：转发编译计划及绑定状态，保留计划的独立副本语义。缺失时，计划绑定校验按未绑定处理，资源执行入口登记失败。
- `UsesAllowAuthorizer() bool`：转发显式放行标记，保持启动清单与放行识别一致。
- `FastDeny(context.Context, string) error`：转发安全提前拒绝检查。缺失时，`CheckFastDeny` 返回 nil，正式授权继续由用例入口执行。

仅嵌入 `IAuthorizer` 会获得其声明的两个方法。其余能力需显式转发，可执行示例及行为验证见 [wrapper_contract_test.go](../../auth/wrapper_contract_test.go)。透明包装沿用内部编译计划的动作、范围和放行语义；会改变决策的领域逻辑应进入 `IPolicy` 或声明规则，由 `Compile` 一并校验 FastDeny 的安全性。

上述要求限定包装已有授权器的能力转发，公共 `IAuthorizer` 继续保持最小方法集。

## 规则与可信事实

`Rule.Actions` 选择动作；空集合表示默认规则。Descriptor 是动作和权限的权威来源。重复规则、未知动作、缺少检查器、无效字段引用及显式配置的空依赖在编译期失败。

| 配置 | 执行语义 |
| --- | --- |
| `Scope` | 权限允许后解析范围条件与权威赋值 |
| `Constraints` | 按声明顺序执行约束，条件取交集，拒绝及错误短路 |
| `OnPermissionDenied` | 仅在权限 Forbidden / Unauthorized 时进入替代路径；已允许权限后的范围拒绝直接返回 |
| `SystemBypassRule` | System 按常规策略放行当前规则，仓储隔离持续执行 |
| `SystemCheckConstraints` | System 放行动作检查后继续执行范围及领域约束 |
| `AllowReason` | 明确放行当前动作，启动清单保存依据；该规则独立于范围、约束与替代规则 |

领域扩展实现 `auth.IPolicy`，由声明引用并交给完整授权内核执行。`auth.WithFacts` 或 `IFactConsumer` 登记 `auth.NewFact` 创建的可信投影。`NewDerivedFact` 从固定父投影读取关联事实，编译器递归登记依赖；未选分支保持父、子提供方未调用。请求字段表达意图，提供方读取可信事实；选中分支通过 `Fact.Get` 按需加载，同一授权执行内复用冻结副本。主体、租户或意图改变会使复用失败。最终写入继续核对事务内状态和版本。

## 具名业务动作与策略组合

规则由构造期的 `Declaration.Rules` 或 `AllPolicies` 组合并冻结，执行阶段固定，不开放任意重排或省略授权的中间件链。授权决定包含范围 Spec 与权威 Assign，而非仅一个 bool。

单资源业务保留 `OrderService.Approve` 等具名方法，内部使用 `Application.Execute`、`ExecuteNew` 或 Bulk。应用统一负责授权、范围内加载、保护字段、版本、事务、钩子与审计。业务不用接触授权凭据、重新包装仓储或传递权限字符串。显式状态 ES 使用受保护的 `StateApplication`，在事件归约后、追加前检查实际最终状态；模型仍是普通状态与确定性事件归约。

原 `app.Command`、Grant、专用读写/仓储包装与 CRUD 特殊授权复用分支已移除，不保留别名。`messaging/command` 的消息命令不受影响。

## 多资源业务编排

跨资源业务由明确的业务服务编排，副作用开始前对全部已知资源执行预检；每个受保护 Application 仍执行自己的正式授权、范围与写入条件检查。预检不是授权决定缓存，也不保证之后策略不会变化，不通过 context 布尔值或裸 Decision 绕过正式判定。

后续正式授权拒绝时，只有各仓储已经证明加入同一数据源、同一事务上下文，才能保证前面的业务写入一并回滚。外部通知、扣款等不可回滚副作用放在 outbox 或提交后阶段。跨数据库、事件流和外部服务不承诺统一事务；跨服务流程使用明确的补偿设计。普通两阶段 Transfer 继续分别授权转出和接收，并保持自身事务入口要求。

复杂读取只向业务暴露经过审查的窄应用服务；服务负责将 Spec 落实到查询、计数和关联读取。仓储与事件追加端口留在组合根和执行器内，不把“插件”当成 Go 进程内任意代码的安全沙箱。共享可信事实如需使用 `PrepareFacts` / `ReuseFacts`，必须在其明确生命周期内 `CloseFacts`，不跨身份、租户或请求意图复用。

### 下游迁移边界

历史 IAM、Alife 等业务曾使用旧 Command API。本次三核心仓协调移除这些公共类型，不表示外部业务已经迁移：下游必须把单资源动作改为普通 Application，跨资源按上述合同重建显式服务编排，并验证原身份、关联、租户、版本、幂等、事务及提交后语义。未取得匹配下游源码时，不声称其已编译或验收。不要在核心新增旧 API 兼容层掩盖迁移缺口。

## 实际入口门禁

Core 的 `auth.ValidateEntries` 将独立注册清单与授权绑定交叉核对。绑定由 Application 的 `AuthorizationBinding(action)`，或同一具名业务对象的 `AuthorizationBinding()` 取得，包含编译计划及条件执行路径。

Runtime `host/authz.EntryRegistry` 连接 HTTP 驱动实际清单与命令、Worker、CLI 的独立注册源。`config.WithEntryValidator(registry)` 在模块路由完成后、后台组件和传输启动前执行门禁。Host 同时把 HTTP 登记能力传给模块路由组；`rest.Mount` 自动继承，嵌套路由保留模块来源。直接挂载的路由组可通过 `http.WithEntryRegistry` 装配同一能力。

手写路由使用 `http.RegisterDeclaredRoute` 登记具体执行器和动作，或由组合根用 `http.BindEntry` 登记已审查的多资源绑定。处理器到实际动作的映射、闭包捕获的仓储及外部客户端访问需要代码审核与行为验证。

清单输出三类状态：

- `declared`：有效资源/动作计划及条件执行绑定。
- `explicit_allow`：有效放行计划或有明确依据的无资源协议入口。
- `unbound_or_invalid`：缺绑定、重复绑定、失效计划或缺条件路径，阻断启用。

HTTP 清单包含预注册、生成、静态和原生注册入口。驱动自身提供的 HEAD 别名与 OPTIONS 协议入口按实际执行分类。成功校验后冻结注册集合；Gin 保留 Engine 引用随后注册的原生新路由会在请求执行前被拒绝，启动也核对清单漂移。新增入口通过新实例重新装配、校验和启用。

命令源使用真实 `RegisteredHandlers` / `SealHandlers` 接入 `host/authz.NewHandlerEntries`。后台任务使用 Core `process/task.Registry` 保存稳定逻辑任务名及实际处理函数；`Register` 拒绝覆盖，封存后拒绝新增，`Run` / `Go` / `GoLoop` 持续调用同一处理函数。动态实例通过上下文提供输入，业务资源操作继续通过同一受保护应用执行。

模块与运行期组件实现 Runtime `host/authz.IEntryProvider.AuthorizationEntries`，提供实际 Source 与 Bindings。Host 在后台启动前调用 `PrepareModuleEntries`，收集模块及 `RuntimeComponents`，固定核对与启动使用的同一实例。已启用入口门禁的运行期组件必须提供实际注册源。所有 EntryValidators 通过后，Host 才调用 `ActivateEntries`；Task Registry 的单独封存保留未启用状态。停止失败会阻止再次启用，清理完成后可以复用同一处理函数重新启动。

独立启动模块或直接使用注册源时，以同一 EntryRegistry 完成 ValidateEntries 和 ActivateEntries；宿主聚合门禁在进入模块 Start 前核对全部来源。Worker 和 CLI 自动覆盖以实际可枚举注册源为边界，任意直接函数及底层仓储访问按项目盘点审核。

项目验收以真实生产组合根的入口报告、受约束仓储调用和实际请求测试为准。HTTP、后台任务、条件中间件及协作服务的计划需要一并核对；构造器测试或单独编译成功不能替代生产启动验收。

## 治理与验证

`GovernanceOptions` 的零配置关闭治理。Snapshot、Audit、Metrics 及失败模式在完整内核统一执行；孤立的 StrictAudit / LenientSnapshot 配置构造失败。SnapshotVersion 记录解析器版本，评估版本绑定和请求内冻结策略属于独立契约。

各受影响仓库分别运行 `GOWORK=off go build ./...`、`go vet ./...` 和 `go test -count=1 ./...`。授权、身份绑定、跨资源预检与事务回滚及 HTTP 启动门禁使用定向 race 验证。Core 的 `GOWORK=off go list -m all` 保持仅输出 `gochen`。
