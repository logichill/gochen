# 数据库 Schema 与迁移

Core `db` 定义数据库、方言和 ORM 契约。迁移执行器、schema 分析和 SQL 持久化位于 `gochen-runtime`；GORM 模型草稿和迁移 CLI 位于 `gochen-contrib`。

## 迁移执行

Runtime `db/migrate` 提供 FileSource / embed FS Source 与 Runner：

- 文件格式为 `000001.schema.name.up.sql` / `.down.sql`，type 可为 schema、seed、demo；短格式 `000001_name.up.sql` 默认属于 schema。
- `NewFileSource` / `NewFS` 创建 source，`NewRunner(database, source, opts...)` 装配运行器。
- Runner 支持 Up、Down、Steps、Migrate、Status、Version、Force，按 `WithMigrationType` 选择迁移链。
- 状态表默认 `schema_migrations`，以 migration_type 为主键分别记录各链版本与 dirty 状态。
- 单个 migration 文件默认在事务中执行；`WithoutTransaction` 用于不能处于事务中的语句，失败仍由 dirty 状态表达。
- 同一状态表中的保留锁行 `__gochen_migration_lock` 承载租约；`WithLockTimeout` 控制等待，`WithLockStaleAfter` 控制失效锁回收，持锁期间续租。
- `Runner.WithLock` 可复用迁移锁执行自定义逻辑，回调中重复调用同一 Runner 的迁移或状态方法会返回冲突。

SQL 分句支持常规 DDL / DML、引号、注释和 PostgreSQL dollar-quoted block。客户端命令如 `\copy` 不属于 Runner 执行范围。迁移状态表必须符合本实现的数据结构。

## Schema 草稿

Runtime `db/schema` 提供 Schema / Table / Column / Index AST，introspect、diff 与 render：

- `diff.Between` 生成新增表、列、索引的操作；`DetectDrifts` 报告类型、nullable、默认值、主键、自增与索引差异及额外对象。
- renderer 输出 up SQL 和反向 down 草稿，不自动执行修改或删除对象。
- AST 不表达完整外键、check、表达式索引等结构；无法表示的对象应体现在 Warnings 中，不能视为已完整对齐。
- 标识符仅接受安全名称；复杂 SQL 表达式不能充当表、列或索引名。
- SQLite 已有表新增无默认值的 NOT NULL 列会被拒绝；MySQL 非主键 AUTO_INCREMENT 列不能由当前 AST 自动安全渲染。这类迁移须显式设计。

Contrib `data/orm/gorm.GenerateMigrationDraft` 从模型生成 AST 并比较当前数据库，`WriteMigrationDraft` 写入 SQL 文件。复杂场景可用带 options 的入口注入 current schema / introspector。

执行前检查 Warnings 和 SQL。生成的 down 草稿含 `ManualReviewGuardStatement`，Runner 在设置 dirty 前识别并拒绝该 guard；人工审核后才可解除。数据回填、收紧约束和破坏性 DDL 的影响由具体迁移方案承担。

## 事件存储

SQL 实现在 Runtime `eventing/store/sqlstore`。以 event_store 为例：

| 字段 / 约束 | 语义 |
| --- | --- |
| `id` | string 事件 ID，唯一 |
| `type` | 稳定事件类型 |
| `aggregate_type` / `aggregate_id` | 共同标识聚合，ID 列类型须与 codec 一致 |
| `version` | 聚合内版本；对 aggregate type + ID + version 建唯一约束 |
| `schema_version` | payload 结构版本 |
| `timestamp` | 事件时间 |
| `payload` / `metadata` | 序列化事件载荷与元数据 |
| `global_position` | 可选全局递增位置 |

建有 global_position 时，还必须建立单列唯一索引及 `event_store_positions` 分配器表，并配置匹配事件表名的 store_name / next_position 行。位置在同一事务内分配，读取以连续高水位控制范围；缺少必需 schema 时写入失败，不在运行期补 DDL。

不使用 global_position 时，全局扫描采用 timestamp / ID 顺序。按事件类型、聚合类型与时间过滤的读取可建立对应复合索引，结合数据库执行计划选择。

int64、string 或强类型聚合 ID 须在 EventStore、Outbox、相关快照与 codec 中一致。使用 string ID 时，应选择匹配的文本列与带 codec 的 SQL store 构造入口。

## Outbox

SQL 实现在 Runtime `eventing/outbox/sqlstore`，默认表为 event_outbox：

- 标识与内容：id、aggregate_id、aggregate_type、event_id、event_type、event_data，event_id 唯一。
- 状态与重试：status、retry_count、last_error、next_retry_at。
- 占用与时间：claim_token、lease_until、created_at、published_at。

为 pending、失败重试、租约到期 claim 及聚合查询设置索引。DLQ、指标和清理组件须与仓储使用同一组表名。状态、claim 与重投规则见 [Outbox](../../eventing/outbox/README.md)。

事件表与 Outbox 的可执行 SQLite DDL 集中在 `gochen-runtime` 仓库 `examples/infra/outbox/sql/internal/schema/schema.go`；生产迁移按目标数据库方言和 ID 类型编写。

## 审计表

Runtime `db/orm/repo.NewAuditStore(orm, table, idGenerator)` 要求已存在的审计表和非空 int64 ID generator。字段为 id、resource_kind、entity_id、operation、operator、timestamp、changes、metadata。

resource_kind 与 entity_id 共同定位业务资源，不能仅按实体 ID 混查不同类型。构造时检查 resource_kind 列；审计 ID 由生成器提供，entity_id 以字符串存储。审计写与业务写使用同一个 ORM 事务 session。

## 投影检查点

Runtime `eventing/projection/sqlstore` 的默认表为 projection_checkpoints：

| 字段 | 语义 |
| --- | --- |
| `projection_name` | 主键，投影名称 |
| `position` | 处理位置 |
| `last_event_id` | 恢复游标 |
| `last_event_time` | 最近事件时间 |
| `updated_at` | 检查点保存时间 |

SQLCheckpointStore 提供显式 CreateTable 入口；生产环境也可由迁移管理。Save 需要事务 session 并只推进位置，重建使用 ForceSave 对齐结果；投影写入须共享该事务。装配示例位于 `gochen-runtime` 仓库 `examples/infra/projection/sql_checkpoint`。
