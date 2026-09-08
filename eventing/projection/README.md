# Projection：读模型与检查点

`IProjection[ID]` 定义 Name、Handle、SupportedEventTypes、Rebuild 与 Status。`ProjectionManager` 负责订阅、处理、恢复、重建和状态记录。

## 装配

`NewProjectionManager[ID](eventStore, eventBus, registry, upgraders)` 与带 config 的构造函数均返回 manager 和 error。Registry / UpgraderRegistry 显式注入；投影的类型注册在启动前完成。

- `RegisterProjection` / `RegisterProjectionWithContext` 注册投影。
- `StartProjection` / `StopProjection` 控制运行。
- `ResumeFromCheckpoint` 从持久位置恢复并启动。
- `RebuildProjection` 重建读模型，完成后保持 stopped，须显式启动。

配置入口为 `ProjectionConfig`；LowLatency、Balanced、HighThroughput 预设控制 checkpoint 保存频率等参数。

## 两种运行方式

| 方式 | 处理路径 | 恢复边界 |
| --- | --- | --- |
| 未启用 checkpoint | EventBus 回调交给投影 Handle | 同一投影不重入；不承诺崩溃恢复 |
| 启用 checkpoint | 在线消息唤醒运行器，从 EventStore 按游标追赶 | 从持久 checkpoint 恢复 |

通过 `WithCheckpointStore` 启用 checkpoint 时必须提供 `IEventStreamStore`。投影同时满足 `ICheckpointingProjection` 和 `IRebuildCheckpointingProjection`，确保增量与重建都能将读模型写入和 checkpoint 保存放在同一原子边界。

普通投影可用 `NewCheckpointingProjector(inner, txRunner)` 包装。Runtime `eventing/projection/sqlstore.NewSQLCheckpointTxRunner(orm)` 提供 ORM 事务执行器，SQLCheckpointStore 读取同一 context 中的事务 session。投影的业务 SQL 也必须使用该事务 session，不能绕回独立连接。

## 游标、幂等与错误

- 同一投影的在线处理、追赶、恢复、重建串行执行；不同投影可并行。
- 运行中使用内存 cursor 跨越尚未持久化的 checkpoint 批次；冷启动从持久位置读取。
- 批量 checkpoint 窗口内崩溃可能重放已处理事件，处理器必须幂等；需要每条事件保存 checkpoint 时设 `CheckpointSaveCount=1`。
- 普通 checkpoint 保存只允许位置前进；重建通过 ForceSave 或同事务重置将位置对齐到重建结果。
- 单事件失败按配置重试，错误不能被当作已处理成功。checkpoint 保存失败应回滚对应事务。
- 持久 cursor 不存在时恢复返回 `NotFound`，由调用方显式安排重建。
- 重建失败后状态为 error，不会留在 rebuilding。

## 示例与验证

在 `gochen-runtime` 仓库执行：

```bash
GOWORK=off go run ./examples/infra/projection/sql_checkpoint
```

SQL 表结构见[数据库 Schema 与迁移](../../docs/guides/db-schema-migration-guide.md#投影检查点)。Core 契约及并发测试位于本包，在 Core 仓库执行 `GOWORK=off go test -count=1 ./eventing/projection`。
