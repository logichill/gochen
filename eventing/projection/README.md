# Projection：读模型与检查点

`IProjection[ID]` 定义 Name、Handle、SupportedEventTypes、Rebuild 与 Status。`ProjectionManager` 负责订阅、处理、恢复、重建和状态记录。

## 装配

`NewProjectionManager[ID](eventStore, eventBus, registry, upgraders)` 与带 config 的构造函数均返回 manager 和 error。Registry / UpgraderRegistry 显式注入；投影的类型注册在启动前完成。

- `IProjectionRegistrar.RegisterProjectionAny` 注册投影，返回绑定本次注册的 `messaging.UnsubscribeFunc`。成功返回非 nil 函数；失败但仍有资源待清理时也会返回函数，调用方必须保留并清理。重复释放成功，旧释放函数不会移除同名的新注册。
- `RegisterProjection` / `RegisterProjectionWithContext` 与按名注销方法供组合根管理；共享 manager 的组件通过上述释放函数管理自身生命周期。
- `SupportedEventTypes` 中重复的事件类型只建立一次订阅。
- `StartProjection` / `StopProjection` 控制运行。
- `ResumeFromCheckpoint` 从持久位置恢复并启动。
- `RebuildProjection` 重建读模型，完成后保持 stopped，须显式启动。

配置入口为 `ProjectionConfig`；LowLatency、Balanced、HighThroughput 预设控制 checkpoint 保存频率等参数。

注销先停用投影并等待在途处理，再释放订阅。清理进行中或失败时，状态为 `cleanup_pending`；失败原因记录在 `LastError`，名称保持占用，Start/Resume/Rebuild 返回 Conflict。重试释放只处理剩余订阅，完成后状态查询返回 NotFound。

等待并发清理或在途处理时遵循释放上下文的取消与超时；停用后等待超时会保留资源供重试，不启动后台清理。底层订阅释放函数也须遵循传入的 context。

`app/eventsourced.EventSourcedAutoRegistrar.RegisterProjections` 返回各次注册的释放函数；即使批次出错也必须处理返回的函数，可交给 `UnregisterProjections(ctx, releases...)` 逆序释放。批量清理会尝试全部释放函数并聚合错误，失败后可重试同一组函数。

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
