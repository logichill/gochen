# ORM 契约

`gochen/db/orm` 定义 ORM、Model、事务 session、查询选项和元信息，不包含物理驱动。标准 SQL 实现在 `gochen-runtime` 仓库 `db/orm/lite`、`db/orm/repo`，GORM 适配在 `gochen-contrib` 仓库 `data/orm/gorm`。

## 接口与能力

| 接口 / 类型 | 职责 |
| --- | --- |
| `IOrm` | 模型入口、上下文派生、Begin / BeginTx、底层 Database |
| `IOrmSession` | 事务会话，提供 Commit / Rollback |
| `IModel` | 模型读写、计数、关联与方言 |
| `IModelWithResult` | 返回受影响行数等 SQL 结果能力 |
| `IAssociation` | 关联维护，显式指定 owner |
| `Capabilities` | 声明适配器真实支持的能力 |
| `ModelMeta` / `FieldMeta` / `AssociationMeta` | 表、字段、关联及标签元信息 |
| `QueryOptions` | 查询条件、排序、分页、预加载、行锁、Join 与 GroupBy |

适配器不能静默忽略不支持的请求，应返回 `errors.Unsupported`。受约束写入依赖准确的受影响行数，装配时必须确认底层能力。

## 字段与 SQL 边界

- `IModel.Dialect()` 必须返回非 nil 的实际数据库方言。无法识别方言时可显式使用 unknown 方言，但不具备标识符引用保证。
- `WithSelect` 只接受安全字段名。受信任 SQL 表达式使用 `WithSelectExprUnsafe`，不能拼接用户输入。
- 表名和列名建议使用 lower_snake；限定名按方言逐段引用。
- ORM 查询选项与 `app/query` 的适配器 Filter 协议职责不同，Application / REST 负责按 QuerySchema 解码，再由仓储映射查询。

Runtime Repo 的 `FilterOpLike` 表示包含字面文本，会转义 `%`、`_` 和反斜杠。Runtime SQLBuilder 的 `IN` slice 占位符会展开参数，空 slice / array 转为恒假条件；Application 查询协议的空 `in` / `not_in` 列表则返回输入错误。

## 仓储约束

租户与范围通过 Runtime Repo 的 `WithIsolation`、`WithResourceKind`、`WithScope` 等显式选项声明。扩展读取从 `ScopedQuery`、`GetWith`、`FindOneWith` 等受限入口继续构造，避免绕过隔离或软删条件。

列名优先级见 [Quick 装配](../../docs/guides/quick-assembly.md#列名约定)，授权链路见[分层授权](../../docs/architecture/layered-authz.md)。事务回调和审计的同事务要求见 [app](../../app/README.md)。

数据库连接、DDL、schema 检查与迁移执行由 Runtime 提供，见[数据库 Schema 与迁移](../../docs/guides/db-schema-migration-guide.md)。
