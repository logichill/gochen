# 资源契约与归属迁移

资源契约定义实体字段如何参与隔离、授权、普通更新和归属迁移。例如，订单的部门字段用于限制可见范围，修改订单金额时部门保持原值，转移部门则使用专门的 Transfer 用例。

L0–L4 的职责和请求链路见 [分层授权](layered-authz.md)。本文说明资源字段和写入的执行规则。

## 实体字段与条件

实体以 `entity:"id"` 标记标识，以 `dim:"department"` 等项目名称标记维度，以 `managed:"true"` 标记框架托管字段。维度支持标量及一层标量指针。

`meta.Ref` 让条件引用字段身份；按字段名和维度名引用同一字段时，两者解析为同一身份。

| 引用 | 示例 | 含义 |
| --- | --- | --- |
| 标识 | `meta.ID()` | 实体 ID |
| 字段 | `meta.ByField("DepartmentID")` | 实体字段 |
| 维度 | `meta.ByDimension("department")` | 标记为 department 的字段 |

`spec.Eq(ref, value)`、`spec.In(ref, values...)`、`spec.IsNull(ref)` 构造条件，`And` / `Or` 组合条件，`All` 表示全部记录，零值表示空集合。NULL 在 Eq/In 中为假，在 IsNull 中为真。

授权条件使用 Ref。仓储业务读查询还可使用 `Named` 条件，由业务显式登记 SQL 或内存回调。关系权限可由授权器展开为 Eq/In，受配置的 IN 上限约束。

## 隔离与资源装配

Runtime Repo 通过构造选项绑定实际资源：

- `WithIsolation(IsolationCols{...})`：启用隔离，默认从 `contextx.TenantID` 取键；自定义 Resolve 时同时指定 Column。
- `WithResourceKind(kind)`：指定授权资源名称。
- `WithResourceContract(contract)`：指定字段隔离声明和维度写能力。
- `WithCrossIsolationReads(audit)`：允许凭请求期凭据执行有留痕的跨隔离读取。

testkit 的 `WithMemoryResourceContract` 与 `WithMemoryIsolationField` 提供对应内存能力；SQL 列名解析由 Runtime 完成，见 [Quick 装配](../guides/quick-assembly.md#列名约定)。

`app.ResourceContract` 的各项含义如下：

| 字段 | 含义 |
| --- | --- |
| `Isolation` | 当前资源的隔离字段引用 |
| `Mutable` | 允许普通编辑的业务分类维度 |
| `Assignable` | 允许授权器在创建或接收迁移时赋值的维度 |
| `Transferable` | 允许通过 Transfer 变更的归属维度 |

全部维度默认受普通更新保护。隔离字段保持结构保护；Mutable 维度用于业务分类，与授权引用、服务端赋值和迁移目标分开配置。

仓储构造生成只读 `ExecutionDescriptor`。授权器的 `Vocabulary.Refs` 声明可能引用的字段，`Vocabulary.Assignable` 声明可能赋值的维度；Application 构造时检查字段绑定、Mutable 冲突、隔离冲突和仓储执行能力。决策返回后还会检查实际条件和赋值，越过声明的决策返回 `Internal`。

同一实体类型可以对应不同资源契约。多个入口写入重叠数据集时，组合根共享授权维度保护基线，管理入口通过 Transfer 改变归属。

## 普通写入

创建依次执行 Before、服务端权威赋值、领域及应用校验、仓储隔离盖戳和目标状态检查。已有维度值与权威赋值冲突时返回 `Forbidden`；业务合法零值由项目规则定义。创建条件独立于尚未生成的 ID。

更新先核对调用方版本并保存受保护字段的独立前值。输入或命令产出后、Before 前后分别检查保护值；修改受保护的归属字段返回 `Forbidden`。

SQL 写入排除保护字段的 SET，并在 WHERE 中同时约束 ID、隔离、有效态、授权条件、版本和保护前值。NULL 保护值使用 IS NULL。零行更新在原授权与隔离范围内重读：不可见返回 `NotFound`，版本不同返回 `Concurrency`，保护值不同返回 `Forbidden`，其余返回存储错误。重读可能观察到后续并发变化，重试前重新读取和授权。

业务变化按独立复制后的真实字段判断，JSON 展示和审计 Projector 控制输出字段。合法无变化更新保持版本；审计关闭时跳过审计快照和 diff，业务比较、保护字段、版本、校验、钩子及事务继续执行。启用审计后序列化失败在事务内回滚。实体复制边界见 [领域建模](../../domain/README.md#实体状态复制)，钩子与事务见 [应用模板](../../app/README.md#hooks-与事务)。

Restore、Purge、ListDeleted 使用专用动作和仓储能力，继续应用隔离与授权。批量普通命令一次授权、一个事务，任一提交前失败整批回滚。

## 两阶段迁移

Transfer 用于显式变更受保护的归属维度。动作声明 `KindTransfer`、`ReceiveAction` 和 `Dimensions`；仓储提供 `ITransferRepository`，资源契约允许相关维度迁移。

`Application.Transfer` 接收调用方 `expectedVersion` 与 `Targets`，入口从事务外开始；已有活动事务时返回 `FailedPrecondition`。

1. 复制并冻结目标意图，确定 OperationID。
2. 转出授权决定哪些来源可以迁出，返回 Using；接收授权决定目标条件和权威赋值，返回 Check / Assign。两次授权使用相同意图。
3. 开启事务，按 ID、隔离、有效态和 Using 读取来源，核对版本。
4. 从来源副本构造候选，只变更获准维度和托管字段，校验领域状态与 Check。
5. 条件 UPDATE 推进版本，在同一事务内按 ID 和隔离重新读取实际行。
6. 比较实际维度与版本，再求值 Check；存储结果漂移时返回 `Internal` 并回滚。
7. 同事务写入转出、转入审计，执行 After，提交后执行 PostCommit。

项目授权器验证目标存在性和接收权限；目标删除或停用竞争由外键或事务内持久化约束保障。合法归属变更同步推进版本。

Runtime `rest.Transfer` 显式注册 POST，请求提供 `Idempotency-Key`、`expected_version` 和 `targets`，版本 0 合法。REST 与命令入口共用 `operation.Runner.ExecuteTransfer`；tracked Runner 要求幂等结果存储和并发预占。重试复用首次编号、结果与 degraded 状态。HTTP 参数和响应见 `gochen-runtime` 仓库 `api/rest/README.md`。

## 历史审计

`AuditRecord.Dimensions` 保存写入时的全部维度，`OperationID` 关联一次逻辑操作。`AuditTrail` 经独立 `audit_read` 授权，按实体 ID 和记录保存的受保护维度过滤；Purge 后仍可查询。Mutable 值可作为历史事实保存，授权引用仍遵循资源契约。

迁移分别写入 `transfer_out` / `transfer_in`，保存各自前后像并共享 OperationID。Changes 可以披露 Projector 允许的维度 Old→New；Projector 和迁移钩子消费独立展示副本。配对和重复记录按操作、资源、实体及动作约束。

Runtime `AuditResource` 绑定资源契约、全部维度列和隔离列，`AuditStore` 构造检查实际表。完整 JSON 与类型化列共同保存，读取检查缺键、类型和一致性；普通字段隔离从写入上下文保存权威归属。跨多个 INSERT 的批量审计要求活动同源事务。

新增维度通过显式历史迁移补齐旧快照。字段 NULL 与快照缺键分别处理，缺失历史事实时明确失败。

事件溯源的 `AuditTrail` 按聚合当前状态授权；普通 ES 命令在回放及并发重试后捕获保护前值。事件历史类型和查询归 `app/eventsourced`，见 [事件溯源应用](../../app/eventsourced/README.md#事件历史)。

## 验证

共享仓储行为矩阵位于 `testkit/contracttest/dimensions.go`；Runtime 的 `db/orm/repo` 测试覆盖 SQL 版本交错、目标状态约束、回读漂移和审计回滚。各仓库独立执行 build、vet、test，并按数据库适配器补充真实查询与事务验证。
