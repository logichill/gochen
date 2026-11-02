# 分层授权与领域维度

Core 通过 auth.IAuthorizer、类型化条件与 Application 编排授权；Runtime 将有效资源契约落实为隔离和条件 SQL，testkit 实现相同的内存语义。

## 能力与落点

| 能力 | 当前入口 |
| --- | --- |
| 用例编排 | app/crud、app/audited、app/eventsourced |
| 动作权限 | auth/action.RBAC |
| 数据隔离 | contextx.TenantID、显式资源隔离绑定 |
| 数据范围 | auth/scoped.New、项目 IAuthorizer / ISpecResolver |
| 决策治理 | auth/governance、Runtime SQL store |

auth 根包提供统一的 Request、Decision、Vocabulary。普通用例在入口授权一次；Transfer 依转出和接收动作授权两次。每次授权结果通过 Spec 显式传给仓储。

## 实体与有效契约

实体以 `entity:"id"` 标记标识，以 `dim:"department"` 等开放名称标记维度，以 `managed:"true"` 标记托管字段。维度支持标量或一层标量指针。字段和维度别名解析为相同字段身份。

`app.ResourceContract` 显式配置 Isolation、Mutable、Assignable、Transferable。全部维度默认受普通更新保护；Mutable 声明可编辑的业务分类。Runtime 的 WithResourceContract 与 testkit 的 WithMemoryResourceContract 生成相同只读 ExecutionDescriptor，Application 在构造期读取并交叉验证。

隔离由 WithIsolation 显式开启，默认键来自 contextx.TenantID；普通字段也可作为隔离字段。auth.TenantEntity 提供可选租户便利类型。资源名通过 WithResourceKind 绑定，列名解析由 Runtime 承担，约定见 [Quick 装配](../guides/quick-assembly.md#列名约定)。

同一实体类型可以有不同资源契约。多个入口写入重叠数据集时，组合根共享授权维度保护基线，管理入口通过 Transfer 改变归属。

## 条件与授权词汇

`meta.ID()`、`meta.ByField(name)`、`meta.ByDimension(dimension)` 构造 Ref，Eq、In、IsNull 接收 Ref。Spec 提供 All、And、Or 等组合；零值匹配零条记录。NULL 在 Eq/In 中为假，在 IsNull 中为真。

Vocabulary.Refs 声明授权条件引用，Vocabulary.Assignable 声明授权器可能输出的赋值维度。Application 构造检查绑定、mutable 冲突、隔离冲突和执行能力；Decision 到达时再次验证实际引用和赋值。

授权条件全部使用 Ref。Named 用于仓储业务读查询，其 SQL 或内存回调由业务显式登记。关系权限可以由授权器展开为 In/Eq，受配置的 IN 上限约束。

配置错误返回 InvalidInput，授权实现产生非法条件或越过词汇返回 Internal，合法策略拒绝返回 Forbidden。不可见实体返回 NotFound，判定故障保留原错误码。

## 普通写入

创建执行 Before、权威赋值、领域及应用校验、仓储隔离盖戳和 WITH CHECK。已有冲突维度值返回 Forbidden；业务合法零值由项目规则定义。创建条件保持独立于尚未生成的标识。

普通更新捕获独立保护前值，在输入或命令产出后、Before 之前以及 Before 之后检查。SQL SET 排除保护字段，WHERE 同时包含标识、隔离、有效态、授权条件、版本和保护前值；NULL 使用 IS NULL。输入改变保护字段返回 Forbidden。

零行写入在原授权和隔离范围内重读：不可见返回 NotFound，版本不同返回 Concurrency，保护值不同返回 Forbidden，其余返回存储错误。失败分类可能观察后续并发，调用方重试前重新读取和授权。

Restore、Purge、ListDeleted 使用专用动作和端口，保持隔离和授权。批量普通命令一次授权、一个事务，任一失败整批回滚。

## 两阶段迁移

迁移动作声明 KindTransfer、ReceiveAction 和 Dimensions，构造期要求仓储提供 ITransferRepository 及对应能力。Application.Transfer 接收调用方 expectedVersion 与 Targets；已有事务中的调用返回 FailedPrecondition。

1. 事务外复制并冻结意图，确定 OperationID。
2. 转出授权返回 Using，接收授权返回 Check 和权威 Assign，两次使用相同意图。
3. 事务内按标识、隔离、有效态和 Using 读取来源，核对版本。
4. 仓储从前像克隆候选，只应用获准维度和更新托管字段，校验领域状态与 Check。
5. 条件 UPDATE 推进版本，然后同事务按标识和隔离回读实际行。
6. 比较全部维度及实际版本，重新求值 Check；存储漂移返回 Internal 并回滚。
7. 同事务写入两侧审计，执行 After 与提交后的 PostCommit。

目标存在性和接收权限由项目授权器验证。目标删除、停用竞争由项目外键或事务内持久化约束保障，所有合法归属变更同步推进版本。

REST 用 rest.Transfer 显式注册 POST。请求必须提供 Idempotency-Key、expected_version 和 targets，版本 0 合法。REST 与命令入口共用 Runner.ExecuteTransfer；tracked Runner 要求幂等存储和并发预占。重试返回首次操作编号和结果，degraded 状态保持在响应及信封中。

## 历史审计

AuditRecord.Dimensions 保存写入时全部维度，OperationID 关联逻辑操作。AuditTrail 经独立 audit_read 授权，使用实体标识和受保护维度过滤历史；Purge 后仍可查询。mutable 值可以保留为历史事实，授权词汇的构造约束保持一致。

迁移写 transfer_out / transfer_in，分别保存前像和实际后像快照，共享 OperationID。两侧 Changes 可以披露 Projector 允许的维度 Old→New；Projector 和迁移钩子收到独立展示副本。配对及重复迁移按操作、资源、实体和动作约束。

Runtime 的 AuditResource 配置有效契约、全部维度物理列及隔离列，AuditStore 构造检查实际表。完整 JSON 与类型化列同时保存，读取检查缺键、类型和一致性。普通字段隔离由写入上下文保存权威归属。跨多个 INSERT 的批量审计要求活动同源事务。

新增维度通过显式历史迁移补齐旧快照。目标字段为 NULL 和快照缺键分别处理；缺失事实明确失败。ES AuditTrail 按当前聚合状态授权，普通 ES 命令在回放及重试后重新捕获保护前值。

## 推荐范围解析与权限目录

Runtime host/authz 提供 NewActionChecker 和 NewScopeResolver(dimension, resolver)。ScopeResolver 将 DataScope 绑定到项目维度；默认读取已绑定范围或 Principal.ActiveScopeID，缺少范围输出 None。

scoped.DataScope 包含 Kind、ScopeIDs 和 TenantID，Filtered 对正数范围去重排序，Global 持续受独立隔离约束，DenyAll 为零值。层级可见集合由项目 IDataScopeResolver 展开。创建及迁移赋值由项目 resolver 明确输出。

组合根通过 Registry/Catalog 登记权限目录，按资源构造授权器。Principal 来自已验证的认证链，客户端身份输入由可信入口解析。

## 跨隔离读取与治理

跨隔离读取同时要求 WithCrossIsolationReads(audit) 与请求 contextx.WithCrossIsolation 的 Reason/Operator 凭据。留痕失败阻断读取，授权 Spec 持续合入查询，写操作保持 Forbidden。项目可以使用独立审计资源记录表级跨隔离读取，保存原资源、表和原因。

auth/governance.Wrap 按 Metrics(Log(Snapshot(base))) 包装根 auth.IAuthorizer。快照默认失败即拒绝；审计默认尽力，WithStrictAudit 改为阻断；指标失败保持业务判定。治理事实通过根 Decision 传递。

## 验证

Core 的 auth、app 与 testkit 和 Runtime db/orm/repo 执行行为测试。共享仓储矩阵位于 gochen/testkit/contracttest/dimensions.go，SQL 专项覆盖版本交错、目标状态约束、回读漂移及审计回滚。示例位于 Runtime examples/security/layering 与 examples/quick。

各仓库独立 build、vet、test，Core 使用 GOWORK=off，Runtime 运行 examples-smoke。完整设计见工作区根 core-design.md 与 ADR-0001。
