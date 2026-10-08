# 事件订阅与投影辅助

本包提供 `EventSourcedTypedHandler`、`EventSourcedProjection` 和 `EventSourcedAutoRegistrar`，组合事件总线、投影与载荷解码。它不属于使用领域仓储或执行聚合命令的必需依赖。

注册批次出错时仍应处理返回的释放函数。`UnregisterHandlers` / `UnregisterProjections` 尝试全部清理并合并错误；失败后可以用同一组释放函数重试。
