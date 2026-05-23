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

下游 `gochen-starter/data/orm/gorm` 的 migration draft 能把 GORM model 转成 `db/schema` AST，并串联：

```go
draft, err := gormorm.GenerateMigrationDraft(ctx, database, &User{})
files, err := gormorm.WriteMigrationDraft("db/migrate", 1, "create users", draft)
```

复杂项目可用 `GenerateMigrationDraftWithOptions` 注入已有 current schema 或自定义 introspector，用于覆盖项目内更完整的外键、约束、索引策略。

生成的 down SQL 是反向草稿，可能包含 `DROP TABLE`、`DROP COLUMN`、`DROP INDEX`。down 文件会带 `migrate.ManualReviewGuardStatement` 保护语句，runner 在置 dirty 前识别该 guard 并拒绝执行；人工 review 后才可删除 guard。

MySQL/Postgres introspect 当前只覆盖基础表、列和“简单列索引”。外键、check、表达式索引、partial index、复杂约束不纳入 AST；SQLite partial/expression index 会跳过并写入 warning；MySQL prefix/descending 索引、Postgres INCLUDE/descending/复杂 key definition 也会跳过并写入 warning，而不是静默降级成普通列列表。Postgres 会通过 `pg_index.indexprs IS NULL` 排除表达式索引，GORM adapter 遇到表达式索引会跳过渲染并写入 warning。生成 draft 后必须 review `draft.Warnings` / 文件注释。

SQL splitter 支持常规 DDL/DML、注释、单/双引号、反引号和 PostgreSQL dollar-quoted block；更复杂的客户端命令（如 `\copy`）仍不属于 runner 执行范围。

对已有表新增 `NOT NULL` 且无默认值的列时，render 仍会输出 SQL 草稿，但 draft 会写入 warning；该 SQL 在历史表已有数据时可能被数据库拒绝，需要人工调整为分阶段迁移。

---

## 1. Event Store 表（`eventing/store/sql`）

gochen 的 `eventing/store/sql` 以如下字段为核心（以 `event_store` 为例）：

- `id`：事件唯一 ID（gochen 事件模型里是 string，因此推荐 `TEXT`/`VARCHAR`）。
- `type`：事件类型（`event.GetType()`）。
- `aggregate_id`：聚合 ID（类型随你的 ID 策略变化）。
- `aggregate_type`：聚合类型（string）。
- `version`：聚合内版本号（乐观锁关键字段）。
- `schema_version`：事件 payload schema 版本（用于 upcast / 滚动升级）。
- `timestamp`：事件时间戳。
- `payload`：事件载荷 JSON（推荐 JSON 或 TEXT）。
- `metadata`：事件元数据 JSON（推荐 JSON 或 TEXT）。

### 1.1 SQLite（示例）

```sql
CREATE TABLE IF NOT EXISTS event_store (
  id             TEXT    PRIMARY KEY,
  type           TEXT    NOT NULL,
  aggregate_id   INTEGER NOT NULL,   -- 若使用 string/UUID，改为 TEXT
  aggregate_type TEXT    NOT NULL,
  version        INTEGER NOT NULL,
  schema_version INTEGER NOT NULL,
  timestamp      DATETIME NOT NULL,
  payload        TEXT    NOT NULL,
  metadata       TEXT    NOT NULL,
  UNIQUE(aggregate_id, aggregate_type, version)
);
```

### 1.2 从 `int64` 迁移到 `string/UUID`（要点）

当你将事件存储从 `ID=int64` 迁移为 `ID=string`（或 UUID）时，最关键的是：

- 将 `event_store.aggregate_id` 从 `INTEGER` 改为 `TEXT`（并同步调整相关索引/唯一约束）。
- 在装配时显式传入 `codec.ICodec[string, any]`，并使用 `sqlstore.NewSQLEventStoreWithCodec[string](...)` 构造（避免不同 driver 的 Scan/Bind 返回类型差异）。

> 提示：如果你的历史数据已经以整数存储，且新系统希望以 string/UUID 对外暴露，可以选择在业务侧做“映射层”（例如对外 string，对内仍 int64），从而避免数据库级迁移。

---

## 2. Outbox 表（`eventing/outbox`）

Outbox 的 SQL 仓储默认使用 `event_outbox` 表（字段含义见 `eventing/outbox/sql_repository.go` 与测试用例）。

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
-- - 请根据你的扫描 SQL（以及方言）选择合适索引。下面给出一个通用组合索引示例。
CREATE INDEX IF NOT EXISTS idx_event_outbox_status_next_retry_at
  ON event_outbox(status, next_retry_at, lease_until);
```

### 2.2 从 `int64` 迁移到 `string/UUID`（要点）

与 Event Store 一致：将 `event_outbox.aggregate_id` 从 `INTEGER` 改为 `TEXT`，并在业务侧为 Outbox 仓储提供对应 ID 形态的实现/构造方式（默认构造函数以 `int64` 为主）。

---

## 3. Projection Checkpoints 表（`eventing/projection`）

投影检查点表默认名为 `projection_checkpoints`，推荐在装配期创建一个 `checkpointStore`，并调用 `checkpointStore.CreateTable(ctx)` 执行建表（该方法会按 dialect 选择兼容 DDL）。

### 3.1 SQLite（示例）

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
