# 事件 Schema 与回放

事件类型、载荷 schema version 和聚合 version 分别表达业务事实类别、载荷结构和聚合事件顺序。保存的事件需要能被当前 reader 正确解释。

## 注册与消费

- 使用稳定的 `EventType()`；载荷结构变化通过 schema version 表达。
- 在显式 Registry 上调用 `RegisterWithVersion(eventType, latest, factory)` 声明当前载荷版本。
- 在 UpgraderRegistry 注册所需的版本升级链。
- 消费时使用 `upcast.HydrateEventPayload` / `DecodeEventPayload[T]`，读取事件自身的 EventSchemaVersion 并完成升级与反序列化。

`app/eventsourced.DomainEventStore.AppendEvents` 使用 Registry 声明的版本写入 schema_version。Registry、UpgraderRegistry 与事件 ID generator 都由组合根注入。

## 发布顺序

涉及事件载荷或事件类型的部署使用 reader-first / writer-second：

1. 先部署能够识别目标类型与 schema 的 reader，包括 registry、upcaster、聚合 handler 和投影。
2. 确认全部消费者可处理后，再启用对应 writer。
3. 检查持久事件、快照和重放入口的读取需求，再决定是否移除过渡代码。

只要存储中还保留需升级的事件，就必须保留相应 upcaster；不能仅因所有在线节点已升级就删除。新增可选字段也应核实实际解码器与消费者语义，不能把“忽略未知字段”当成通用保证。

## 回放约束

聚合自动路由按 Go 事件类型匹配 handler。`RestoreAggregate` 在事件未命中 handler 时返回错误，避免跳过事件后继续推进版本。

遇到无法回放的事件应补齐注册、升级器或 handler，必要时编写明确的数据迁移方案。不得以忽略未知事件的方式使聚合表面恢复成功。

聚合装配见[事件溯源速查](ddd-eventsourcing-quick-reference.md)，表结构见[数据库 Schema 与迁移](../guides/db-schema-migration-guide.md)。
