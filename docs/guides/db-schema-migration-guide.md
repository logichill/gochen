# DB Schema Migration Guide（gochen）

本文档用于补齐 gochen 文档中提到的“SQL schema / 迁移实践”的最小可用参考。

> 注意：gochen 侧提供的是接口与参考实现；不同业务方会根据数据库方言、索引策略、分表分库等做调整。本文只覆盖与当前仓库实现强相关的表结构要点。

---

## 0. Migration Runner 与 Schema Draft

`db/migrate` 提供最小 SQL migration runner：

- 文件命名：推荐显式 type 格式 `000001.schema.name.up.sql` / `000001.seed.name.up.sql` / `000001.demo.name.up.sql`；兼容短格式 `000001_name.up.sql` / `000001_name.down.sql`，短格式默认归属 `schema` type。
- source：`migrate.NewFileSource(dir)` 或 `migrate.NewFS(embedFS, dir)`。
- runner：`migrate.NewRunner(database, source)`，支持 `Up`、`Down`、`Steps`、`Migrate`、`Status`、`Version`、`Force`。
- 状态表默认 `schema_migrations`。
- 并发保护：`runner.WithLock(ctx, fn)` 使用 `schema_migrations` 中的保留锁行提供数据库级互斥，不再依赖独立锁表；进程异常退出后，超过 `migrate.WithLockStaleAfter(...)`（默认 15s）的 stale lock 会自动回收。保留 migration type `__gochen_migration_lock` 仅供锁行使用；`WithLock` 回调期间若再次调用同一个 `Runner` 的状态/迁移方法，会快速返回冲突错误而不是死锁。
- 事务行为：默认按单个 migration 文件包裹事务；遇到不允许在事务内执行的方言语句时，可通过 `migrate.WithoutTransaction()` 禁用事务包装，由 dirty 状态记录失败版本。
- 状态表按 migration type 分别记录版本，默认 type 为 `schema`；可用 `migrate.WithMigrationType("seed")` / `migrate.WithMigrationType("demo")` 分开管理 seed 数据、demo 数据等迁移版本。Runner 只执行与自身 type 匹配的 migration 文件。
- gochen migration 不接管旧 `golang-migrate` 状态表；若目标库已有旧格式 `schema_migrations` / `demo_migrations`，需在切换前人工清理或重建目标库。

`db/schema` 提供最小 schema AST、introspect、diff、render：

- AST 当前只覆盖 `Schema/Table/Column/Index`。
- `Schema.Warnings` 会携带 introspect 过程中发现的覆盖范围限制或被跳过的复杂结构提示。
- `diff.Between(current, desired)` 只生成新增表、列、索引这类 additive 变更。
- `diff.DetectDrifts(current, desired)` 会报告同名列/索引的类型、nullable、default、primary key、auto increment、unique、索引列等差异，也会报告当前库存在但 desired 未声明的表、列、索引；比较 default 时会对常见字符串字面量、Postgres 顶层 `::type` cast 与自增列底层 `nextval(...)` 做归一化，减少伪 drift；仍不会自动生成修改/删除 SQL。
- `render.RenderSQL` 渲染 up SQL；`render.RenderDownSQL` 渲染反向 down 草稿。
- MySQL 的非主键 `AUTO_INCREMENT` 列不会自动生成 SQL；当前 AST 不表达“列已是 key 但非主键”的安全前提，遇到这类字段会返回错误，需人工设计迁移步骤。
- render 只接受安全数据库标识符（字母/数字/下划线和点分段），GORM tag 或手工 AST 中的复杂表达式不会被当作表/列/索引名渲染。

下游 `gochen-contrib/data/orm/gorm` 的 migration draft 能把 GORM model 转成 `db/schema` AST，并串联：

```go
draft, err := gormorm.GenerateMigrationDraft(ctx, database, &User{})
files, err := gormorm.WriteMigrationDraft("db/migrate", 1, "create users", draft)
```

复杂项目可用 `GenerateMigrationDraftWithOptions` 注入已有 current schema 或自定义 introspector，用于覆盖项目内更完整的外键、约束、索引策略。

生成的 down SQL 是反向草稿，可能包含 `DROP TABLE`、`DROP COLUMN`、`DROP INDEX`。down 文件会带 `migrate.ManualReviewGuardStatement` 保护语句，runner 在置 dirty 前识别该 guard 并拒绝执行；人工 review 后才可删除 guard。

MySQL/Postgres introspect 当前只覆盖基础表、列和“简单列索引”。外键、check、表达式索引、partial index、复杂约束不纳入 AST；SQLite partial/expression index 会跳过并写入 warning；MySQL prefix/descending 索引、Postgres INCLUDE/descending/复杂 key definition 也会跳过并写入 warning，而不是静默降级成普通列列表。Postgres 会通过 `pg_index.indexprs IS NULL` 排除表达式索引，GORM adapter 遇到表达式索引会跳过渲染并写入 warning。生成 draft 后必须 review `draft.Warnings` / 文件注释。

SQL splitter 支持常规 DDL/DML、注释、单/双引号、反引号和 PostgreSQL dollar-quoted block；更复杂的客户端命令（如 `\copy`）仍不属于 runner 执行范围。

SQLite 对已有表新增 `NOT NULL` 且无默认值的列时，`render.RenderSQL` 会直接返回错误，不生成草稿 SQL；需要补默认值、先新增 nullable 列后回填并收紧约束，或人工编写迁移。其他方言下生成的 draft 仍需人工 review 数据兼容性。

---

## 1. Event Store 表（`eventing/store/sql`）

gochen 的 `eventing/store/sql` 以如下字段为核心（以 `event_store` 为例）：

- `id`：事件唯一 ID（gochen 事件模型里是 string，因此推荐 `TEXT`/`VARCHAR`）。
- `type`：事件类型（`event.GetType()`）。
- `aggregate_id`：聚合 ID（类型随你的 ID 策略变化）。
- `aggregate_type`：聚合类型（string）。
- `version`：聚合内版本号（乐观锁关键字段）。
- `schema_version`：事件 payload schema 版本（用于 upcast / 滚动升级）。
- `global_position`：可选的全局递增位置；一旦建列，迁移必须同时创建唯一索引与 `event_store_positions` 分配器行，SQL store 写路径会校验该 schema 能力，缺失时 fail-fast，但不会在运行期自动执行 DDL。写入时在同一事务内从分配器表预留连续位置；订阅读取仍采用 high-water-mark 兜底，仅读到首个连续区段末位置（gap 之前）。
- `timestamp`：事件时间戳。
- `payload`：事件载荷 JSON（推荐 JSON 或 TEXT）。
- `metadata`：事件元数据 JSON（推荐 JSON 或 TEXT）。

聚合身份由 `(aggregate_type, aggregate_id)` 共同定义；不同聚合类型可以复用同一个 `aggregate_id`，版本号也分别在各自聚合内递增。

### 1.1 SQLite（示例）

```sql
CREATE TABLE IF NOT EXISTS event_store (
  id             TEXT    PRIMARY KEY,
  type           TEXT    NOT NULL,
  aggregate_id   INTEGER NOT NULL,   -- 若使用 string/UUID，改为 TEXT
  aggregate_type TEXT    NOT NULL,
  version        INTEGER NOT NULL,
  schema_version INTEGER NOT NULL,
  global_position INTEGER NULL,
  timestamp      DATETIME NOT NULL,
  payload        TEXT    NOT NULL,
  metadata       TEXT    NOT NULL,
  UNIQUE(aggregate_id, aggregate_type, version)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_event_store_global_position_unique
  ON event_store(global_position);

-- 事件流扫描索引建议：
-- - 投影/订阅的 StreamAggregate 按 timestamp,id 稳定排序，并用 type/aggregate_type/timestamp 做过滤；
-- - 无 global_position 时走 (timestamp,id) 排序，带过滤的流走对应复合索引；
-- - 启用 global_position 时，游标分页由上面的唯一索引支撑，但 type/aggregate_type 过滤仍可受益于复合索引；
-- - 生产库应结合方言与数据分布验证执行计划。
CREATE INDEX IF NOT EXISTS idx_event_store_stream_time
  ON event_store(timestamp, id);
CREATE INDEX IF NOT EXISTS idx_event_store_stream_type
  ON event_store(type, timestamp, id);
CREATE INDEX IF NOT EXISTS idx_event_store_stream_aggregate_type
  ON event_store(aggregate_type, timestamp, id);

CREATE TABLE IF NOT EXISTS event_store_positions (
  store_name    TEXT PRIMARY KEY,
  next_position INTEGER NOT NULL CHECK (next_position > 0)
);

INSERT OR IGNORE INTO event_store_positions (store_name, next_position)
  VALUES ('event_store', 1);
```

`store_name` 使用 SQL store 配置的事件表名；若配置为 `main.event_store` 这类限定名，分配器行也应使用同一个值。不需要全局流顺序时可以不建 `global_position` 列及 `event_store_positions`。stream horizon 冷启动会扫描唯一索引确认首个 gap；后续过期或本实例 append 失效时会保留上次单调 horizon，只从该位置向后增量检查。

### 1.2 从 `int64` 迁移到 `string/UUID`（要点）

当你将事件存储从 `ID=int64` 迁移为 `ID=string`（或 UUID）时，最关键的是：

- 将 `event_store.aggregate_id` 从 `INTEGER` 改为 `TEXT`（并同步调整相关索引/唯一约束）。
- 在装配时显式传入 `codec.ICodec[string, any]`，并使用 `sqlstore.NewSQLEventStoreWithCodec[string](...)` 构造（避免不同 driver 的 Scan/Bind 返回类型差异）。

> 提示：如果你的历史数据已经以整数存储，且新系统希望以 string/UUID 对外暴露，可以选择在业务侧做“映射层”（例如对外 string，对内仍 int64），从而避免数据库级迁移。

---

## 2. Outbox 表（`eventing/outbox`）

Outbox 的 SQL 仓储位于 `eventing/outbox/sqlstore`，默认使用 `event_outbox` 表（字段含义见 `eventing/outbox/sqlstore/sql_repository.go` 与测试用例）。

### 2.1 SQLite（示例）

```sql
CREATE TABLE IF NOT EXISTS event_outbox (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  aggregate_id INTEGER NOT NULL,     -- 若使用 string/UUID，改为 TEXT
  aggregate_type TEXT NOT NULL,
  event_id     TEXT NOT NULL UNIQUE,
  event_type   TEXT NOT NULL,
  event_data   TEXT NOT NULL,        -- JSON string
  status       TEXT NOT NULL DEFAULT 'pending',
  claim_token  TEXT NOT NULL DEFAULT '',
  created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  published_at DATETIME NULL,
  retry_count  INTEGER NOT NULL DEFAULT 0,
  last_error   TEXT NULL,
  lease_until  DATETIME NULL,
  next_retry_at DATETIME NULL
);

-- 索引建议：
-- - 典型 Outbox 扫描会按 status 过滤，并按 next_retry_at/lease_until/created_at 做“可发布/可重新 claim”的时间窗口筛选；
-- - Claim 查询按 created_at, id 稳定排序；生产库应结合方言和数据分布验证执行计划。
CREATE INDEX IF NOT EXISTS idx_event_outbox_pending_claim
  ON event_outbox(status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_event_outbox_retry_claim
  ON event_outbox(status, next_retry_at, created_at, id);
CREATE INDEX IF NOT EXISTS idx_event_outbox_lease_claim
  ON event_outbox(status, lease_until, created_at, id);
CREATE INDEX IF NOT EXISTS idx_event_outbox_aggregate
  ON event_outbox(aggregate_id, aggregate_type);
```

### 2.2 从 `int64` 迁移到 `string/UUID`（要点）

与 Event Store 一致：将 `event_outbox.aggregate_id` 从 `INTEGER` 改为 `TEXT`，并在业务侧通过 `eventing/outbox/sqlstore` 为 Outbox 仓储提供对应 ID 形态的实现/构造方式（默认构造函数以 `int64` 为主）。

---

## 3. Audit 表（`db/orm/repo.AuditStore`）

`db/orm/repo.NewAuditStore(...)` 会在构造期检查 audit 表是否包含 `resource_kind` 列；缺失时直接返回 `InvalidInput`，不会在运行期自动加列。升级前必须先完成表结构迁移。

核心字段：

- `id`：审计记录 ID；构造 `db/orm/repo.AuditStore` 时必须显式传入 `gen.IGenerator[int64]`，框架不再读取包级默认生成器。
- `resource_kind`：资源类型隔离字段；共享 audit 表按该列区分不同实体/资源。
- `entity_id`：被审计实体 ID，统一按 string 存储。
- `operation`：操作类型（create/update/delete/restore/purge 等）。
- `operator`：操作者。
- `timestamp`：审计时间。
- `changes` / `metadata`：JSON 文本。

### 3.1 SQLite（新增表示例）

```sql
CREATE TABLE IF NOT EXISTS audit_records (
  id            INTEGER PRIMARY KEY,
  resource_kind TEXT NOT NULL DEFAULT '',
  entity_id     TEXT NOT NULL,
  operation     TEXT NOT NULL,
  operator      TEXT NOT NULL,
  timestamp     DATETIME NOT NULL,
  changes       TEXT NOT NULL DEFAULT '{}',
  metadata      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_audit_records_resource_entity_time
  ON audit_records(resource_kind, entity_id, timestamp DESC, id DESC);
```

### 3.2 旧表补列与回填

旧表已有审计数据时，先补 nullable/default 列，再按业务资源回填：

```sql
ALTER TABLE audit_records
  ADD COLUMN resource_kind TEXT NOT NULL DEFAULT '';

-- 若该 audit 表只承载一种实体，直接回填为固定资源类型。
UPDATE audit_records
SET resource_kind = 'User'
WHERE resource_kind = '';

CREATE INDEX IF NOT EXISTS idx_audit_records_resource_entity_time
  ON audit_records(resource_kind, entity_id, timestamp DESC, id DESC);
```

若同一旧 audit 表混放多个资源类型，必须先用业务可验证规则把历史行拆分/回填到正确 `resource_kind`；不能可靠判定的历史数据应人工处理后再升级。升级后新写入会从 audited application 的 `ResourceKind()` / audit context 写入该字段，读路径也会按当前资源类型过滤，避免不同资源共用 `entity_id` 时串读。

---

## 4. Projection Checkpoints 表（`eventing/projection`）

投影检查点表默认名为 `projection_checkpoints`，推荐在装配期创建一个 `checkpointStore`，并调用 `checkpointStore.CreateTable(ctx)` 执行建表（该方法会按 dialect 选择兼容 DDL）。

### 4.1 SQLite（示例）

```sql
CREATE TABLE IF NOT EXISTS projection_checkpoints (
  projection_name TEXT PRIMARY KEY,
  position INTEGER NOT NULL DEFAULT 0,
  last_event_id TEXT NOT NULL DEFAULT '',
  last_event_time DATETIME NULL,
  updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_projection_checkpoints_updated_at ON projection_checkpoints(updated_at);
```
