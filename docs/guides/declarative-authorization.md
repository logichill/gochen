# 强类型授权接入

资源、动作、权限码及范围规则由 Go 声明连接，组合根调用 `auth.Compile` 冻结计划并注入执行入口。声明随着代码构建和部署；部署配置继续控制认证开关及物理驱动，策略动态热更新沿用独立设计边界。

可运行入口：

- [纯 Core 示例](../../examples/auth/declaration/main.go)：声明编译、Application 读写范围与独立命令注册源核对。
- Runtime 的 `examples/auth/authorization`：可信权限及 DataScope 装配。
- Runtime 的 `examples/quick`：Quick Application 与 HTTP 装配。

## Application

1. 从仓储取得 `ExecutionDescriptor().Metadata()`，准备 Application Options。
2. 使用 `crud.Describe`、`audited.Describe` 或 `eventsourced.Describe` 取得与实际执行同源的 Descriptor。
3. 在 `auth.Declaration` 中绑定 Descriptor、`ResourceAccessData` 和规则。
4. `auth.Compile` 接收权限检查器、实体元数据及可选治理配置；将生成的授权器注入 Application。

Runtime Quick 的 `DeclaredApp` 完成基础 CRUD 声明装配；审计型与事件溯源 Application 使用各自的 Describe、Compile 与构造入口。具体签名与约束见各函数 GoDoc。

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

## 多资源命令

`app.NewCommand` 连接强类型 Input、资源声明、Access 装配及 Handler。每个 `CommandResource` 指定实际仓储、已编译授权器、动作及意图投影。全部适用资源授权成功后，才装配 Access 并执行 Handler。

基础 `Application.Execute` / `ExecuteNew` / Bulk 保留单资源或同类聚合的事务、版本、钩子及审计执行链。`app.Command` 提供自定义资源访问的授权与能力生命周期，不自动建立跨资源事务，也不替代业务审计或提交后副作用处理。跨仓储或跨事件存储操作应逐一核实同源事务加入能力，不能因外层存在事务就宣称全部写入原子提交。

命令内继续使用 CRUD / Audited 执行链时，将对应资源的 `CommandGrant` 写入该次构造的 `crud.Options.CommandGrant`（Audited Options 同样嵌入此配置）。应用复用此次完整 Decision，包括 Spec 与创建赋值；构造时核对实际仓储实例、同一编译授权器和动作计划，执行时核对主体、租户绑定、动作、目标 ID 与命令有效期。嵌套应用持续执行事务、保护字段、版本、钩子及审计。该应用的使用范围限定在本次命令及获准动作内。

携带 `CommandGrant` 的 CRUD / Audited 应用在 `Transfer` 入口返回 `FailedPrecondition`，在命令内及命令结束后均保持此约束。普通两阶段迁移由独立应用执行，继续分别授权转出与接收。

IAM Role、User HTTP 既有实体写入及 Group 普通／HTTP CRUD 复用主资源 CommandGrant。更新命令在 Core 事务内冻结治理前像，更新钩子复用该快照，每次执行完成一次主资源判定与一次当前实体读取。相关父对象、目录、子树及关联投影继续使用独立声明能力。SearchRoles / ActiveRoles 保留同租户完整目录，用户与组展示由返回角色的真实关系及独立声明能力派生；普通 CRUD 列表继续落实 DataScope。用户关联分别落实角色快照与用户条件，事件在最外层提交后发布；初始化声明创建、完整名称目录及 Tenant / RootScope 能力。更新时，正数 namespace 查询为 NotFound 后使用可信租户根校验权限定义，原 namespace ID 持续保留。

RoleUsers 通过命令注册源角色 Fact，沿用其目标授权并在后续能力中复用一次读取的事实。成员使用独立用户条件，嵌套组使用独立组条件，嵌套角色展示保留同租户真实用户角色关系。各关联能力限定为可信源角色或实际返回的用户 ID；命令结束后全部撤销。RoleGroups 独立落实源角色与关联组织的范围条件。RoleUsers 的现有 HTTP 路由绑定同一多资源命令；RoleGroups 沿用普通服务入口。Runtime `ScopedQueryWithSpec` 为框架生成的列条件添加主表限定，确保 JOIN 中身份、隔离及软删同名列保持正确归属。

`app.CommandRepository` 在命令生命周期内执行条件读取、锁定读取、创建、更新、删除、查询和计数。它核对仓储实例、请求 ID、身份绑定、可信读取前值和版本；执行结束后访问能力失效。`When` 可依据前序资源的可信事实决定是否访问后续资源，启动绑定同时记录条件路径。

资源专属列表、统计和聚合使用 `CommandReader`：组合根绑定同一实际仓储及条件读取函数，框架传递授权 Spec 和请求 ID 条件，隔离输入、结果并核对能力生命周期。全局标识资源的服务端口可使用 `CommandIdentityCall`：只接受 ID 条件及空 Assign，服务调用前核对真实目标 ID，组合根负责把该 ID 连接到实际端口。

`CommandResource.ReuseFactsFrom` 显式引用已授权前序资源。它通过 `Authorizer.ReuseFacts` 复用两份计划共同注册、已经加载的同一提供方结果，要求身份、租户、IDs 与 Targets 一致。当前资源继续独立授权和治理。提供方只在跨资源目标语义一致时共享实例；未加载结果继续按当前资源的选中分支解析。

Command 在成功或失败结束时调用 `Request.CloseFacts`，同步撤销事实会话与资源能力。保留请求在执行结束后调用 Fact.Get 会失败，未选投影继续保持未读取；已经取得的独立投影可继续使用。直接使用 PrepareFacts 的执行骨架在自身生命周期结束时调用 CloseFacts。

订单派生实例位于 Alife `internal/shop/service/order/order_command.go`：订单及商品分别登记资源计划，商品 ID、租户和管理范围必须与可信订单关联相符；锁定商品后再次核对关系。积分保持全局 UserID 模型，独立声明为订单派生资源，通过 `CommandIdentityCall` 限定可信订单用户，沿用本人、家庭归属及主订单授权。订单、库存和积分复用冻结订单事实；查询和统计以 `CommandReader` 落实用户与管理范围条件。

Alife 的 HTTP 用户购买、详情和动态订单动作分别绑定 `http_purchase`、`http_read`、`http_action`。用户入口额外核对现有用户/家庭访问关系；动态动作允许取消、退款和完成，退款及完成持续要求管理权限。完成动作跳过库存和积分计划；退款待收敛状态跳过库存并继续幂等积分恢复。

商品目录动态查询绑定 `catalog_query`，通过 CommandReader 支持投影及同条件计数，显式记录租户内目录可见规则；仓储隔离持续执行。商品上架、下架和库存调整绑定 `http_action` 的受管理命令。商城服务构造时要求实际条件查询能力，全部手写路由与管理端 REST 纳入独立入口核对。

## 实际入口门禁

Core 的 `auth.ValidateEntries` 将独立注册清单与授权绑定交叉核对。绑定由 Application 或 Command 的 `AuthorizationBinding(action)` 取得，包含编译计划及条件执行路径。

Runtime `host/authz.EntryRegistry` 连接 HTTP 驱动实际清单与命令、Worker、CLI 的独立注册源。`config.WithEntryValidator(registry)` 在模块路由完成后、后台组件和传输启动前执行门禁。Host 同时把 HTTP 登记能力传给模块路由组；`rest.Mount` 自动继承，嵌套路由保留模块来源。直接挂载的路由组可通过 `http.WithEntryRegistry` 装配同一能力。

手写路由使用 `http.RegisterDeclaredRoute` 登记具体执行器和动作，或由组合根用 `http.BindEntry` 登记已审查的多资源绑定。处理器到实际动作的映射、闭包捕获的仓储及外部客户端访问需要代码审核与行为验证。

清单输出三类状态：

- `declared`：有效资源/动作计划及条件执行绑定。
- `explicit_allow`：有效放行计划或有明确依据的无资源协议入口。
- `unbound_or_invalid`：缺绑定、重复绑定、失效计划或缺条件路径，阻断启用。

HTTP 清单包含预注册、生成、静态和原生注册入口。驱动自身提供的 HEAD 别名与 OPTIONS 协议入口按实际执行分类。成功校验后冻结注册集合；Gin 保留 Engine 引用随后注册的原生新路由会在请求执行前被拒绝，启动也核对清单漂移。新增入口通过新实例重新装配、校验和启用。

命令源使用真实 `RegisteredHandlers` / `SealHandlers` 接入 `host/authz.NewHandlerEntries`。后台任务使用 Core `process/task.Registry` 保存稳定逻辑任务名及实际处理函数；`Register` 拒绝覆盖，封存后拒绝新增，`Run` / `Go` / `GoLoop` 持续调用同一处理函数。动态实例通过上下文提供输入，业务资源操作继续在 Command 内执行。

模块与运行期组件实现 Runtime `host/authz.IEntryProvider.AuthorizationEntries`，提供实际 Source 与 Bindings。Host 在后台启动前调用 `PrepareModuleEntries`，收集模块及 `RuntimeComponents`，固定核对与启动使用的同一实例。已启用入口门禁的运行期组件必须提供实际注册源。所有 EntryValidators 通过后，Host 才调用 `ActivateEntries`；Task Registry 的单独封存保留未启用状态。停止失败会阻止再次启用，清理完成后可以复用同一处理函数重新启动。

独立启动模块或直接使用注册源时，以同一 EntryRegistry 完成 ValidateEntries 和 ActivateEntries；宿主聚合门禁在进入模块 Start 前核对全部来源。Worker 和 CLI 自动覆盖以实际可枚举注册源为边界，任意直接函数及底层仓储访问按项目盘点审核。

Alife 对账任务绑定订单身份投影扫描、结算收敛及取消补偿的全部资源计划。扫描通过 CommandReader 传递 Spec，仓储采用已有 CrossIsolation 只读凭据及留痕，逐订单写入重新绑定租户与管理范围。IAM 吊销清理把现有全局存储声明为固定 access-token ledger，只有无租户的可信 System 可以取得该整体资源的过期清理能力，存储沿用过期后五分钟保留及有界批次规则。两类后台任务均在执行前完成登记和核对。

项目验收以真实生产组合根的入口报告、受约束仓储调用和实际请求测试为准。HTTP、后台任务、条件中间件及协作服务的计划需要一并核对；构造器测试或单独编译成功不能替代生产启动验收。

## 治理与验证

`GovernanceOptions` 的零配置关闭治理。Snapshot、Audit、Metrics 及失败模式在完整内核统一执行；孤立的 StrictAudit / LenientSnapshot 配置构造失败。SnapshotVersion 记录解析器版本，评估版本绑定和请求内冻结策略属于独立契约。

各受影响仓库分别运行 `GOWORK=off go build ./...`、`go vet ./...` 和 `go test -count=1 ./...`。授权、身份绑定、命令能力生命周期及 HTTP 启动门禁使用定向 race 验证。Core 的 `GOWORK=off go list -m all` 保持仅输出 `gochen`。
