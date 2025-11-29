# orm 包设计概览

本包只提供 ORM 抽象接口与模型元信息描述，不包含任何 ORM 行为实现。业务侧可通过适配器模式将具体 ORM（如 GORM、Ent、SQLX 自定义层）接入 gochen。

## 设计要点
- **接口而非实现**：定义 `IOrm/IOrmSession/IModel/IAssociation`（关联需传入 owner 对象），业务侧实现适配器。
- **能力检测**：`Capabilities` 声明支持的能力，超出能力的调用应返回明确错误（建议使用 `orm.ErrUnsupported`）。
- **元信息承载**：`ModelMeta/FieldMeta/AssociationMeta` 传递表名、字段、关联等描述，`Tags` 可存放原始 `orm`/`gorm` 等标签字符串，避免编译期改动。
- **轻量查询选项**：`QueryOptions` 覆盖常见的筛选/排序/分页/预加载/行锁/Join/GroupBy 需求，适配器自行解释或报不支持。

## 推荐适配模式
1) 在业务仓库实现适配器（例如 `GormAdapter`），满足上述接口并声明支持能力。
2) 通过 `ModelMeta` 注册模型描述，必要时解析 `Tags["orm"]` 中的原始内容映射到目标 ORM。
3) 当调用方请求超出能力时返回错误（例如返回 `orm.ErrUnsupported`），避免静默降级。

后续可在业务侧补充示例/contract tests 验证适配器行为，本包不承担具体 ORM 逻辑。
