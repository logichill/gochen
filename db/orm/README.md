# ORM 抽象（db/orm）

本包只提供 ORM 抽象接口与模型元信息描述，不包含任何 ORM 行为实现。业务侧通过适配器模式将具体 ORM（如 GORM、Ent、SQLX 自定义层）接入 gochen。

边界：
- 这里不负责连接管理、迁移、事务语义或任何具体 ORM 细节；
- 这里的目标是让 repo/service 层“只依赖抽象”，把基础设施细节留在业务仓库。

## 设计要点
- **接口而非实现**：定义 `IOrm/IOrmSession/IModel/IAssociation`（关联需传入 owner 对象），业务侧实现适配器。
- **能力检测**：`Capabilities` 声明支持的能力，超出能力的调用应返回明确错误（建议使用 `errors.NewUnsupportedOperationError("orm: capability unsupported")` 或类似错误）。
- **元信息承载**：`ModelMeta/FieldMeta/AssociationMeta` 传递表名、字段、关联等描述，`Tags` 可存放原始 `orm`/`gorm` 等标签字符串，避免编译期改动。
- **轻量查询选项**：`QueryOptions` 覆盖常见的筛选/排序/分页/预加载/行锁/Join/GroupBy 需求，适配器自行解释或报不支持。

## 推荐适配模式
1) 在业务仓库实现适配器（例如 `GormAdapter`），满足上述接口并声明支持能力。
2) 通过 `ModelMeta` 注册模型描述，必要时解析 `Tags["orm"]` 中的原始内容映射到目标 ORM。
3) 当调用方请求超出能力时返回错误（例如返回 `errors.NewUnsupportedOperationError(...)`），避免静默降级。

## 标识符约定（方言/大小写）

本仓库对表名/列名/别名的“输入约束”主要是安全性（避免注入），并不会自动解决所有数据库方言差异。

推荐强约定：
- 表名/列名/别名使用 `lower_snake`（仅小写字母、数字、下划线，必要时用 `schema.table` 这种点分段形式）。
- 避免使用保留字与大小写敏感标识符（需要依赖引号的场景），否则不同适配器/方言下很容易出现“某些片段被 quote、某些片段未 quote”导致的运行时错误。

特别说明（`db/orm/lite` 适配器）：
- SELECT/ORDER BY 会对“看起来是标识符”的列按方言 quote；
- JOIN 场景会拼接表表达式并走 `FromUnsafe()`，因此会在 JOIN 表达式里按方言 quote 标识符；
- WHERE 表达式仍由调用方提供（原样透传），若自行拼接了大小写敏感/需要引号的标识符，请确保与其他片段的 quoting 策略一致。

后续可在业务侧补充示例/contract tests 验证适配器行为，本包不承担具体 ORM 逻辑。
