# 事件 Schema 演进与滚动升级

事件溯源系统的"线上可演进性"核心在于：**滚动升级期间，旧/新版本同时在线时仍能正确读写同一条事件流**。

这不是一份 API 速查，而是一组需要长期遵守的纪律。如果你不遵守，最终事件流会以"新版本写的数据旧版本读不懂"或者"旧版本写的数据新版本解读错"的形式咬你一口。

## 1. Reader-first / Writer-second

每次事件 schema 变更，按这个顺序来：

1. **先升级读端**：发布"能读旧数据 + 能读新数据"的版本（包含 registry、upcaster、handler）。
2. **再升级写端**：通过开关或灰度逐步开启新写入（新字段、新 schemaVersion、新事件类型）。
3. **最后清理**：当全量节点都能消费新写入后，再清理兼容逻辑（老字段、老事件类型、临时 upcaster）。

这条纪律的核心是：**永远不要让写端超前于读端**。任何时间点，在线集群里每一台机器都必须能读懂当前正在被写入的事件。

## 2. 推荐：保持 eventType 稳定，用 schema_version 演进

尽量避免"改 eventType 或换 Go 类型名即升级"的做法。推荐用稳定的 `EventType()` 字符串 + `schema_version` 演进：

- **增加字段**：优先增加可选字段。旧 reader 向后兼容——JSON 会忽略未知字段。
- **变更语义或重命名字段**：用 `eventing/upcast` 在读取边界将旧数据升级为新 schema。
- **声明最新版本**：在组合根使用 `registry.RegisterWithVersion(eventType, latest, factory)`。
- **补齐升级链**：在 `upcast.UpgraderRegistry` 注册 `v1 -> v2 -> ... -> latest` 的升级器链。

`app/eventsourced.DomainEventStore.AppendEvents` 会按 registry 的最新版本写入 `schema_version`，避免"载荷已经是新结构但 schema_version 还是旧值"导致升级器误判。

## 3. 引入新事件类型要更谨慎

引入全新的 `eventType` 时，旧版本 reader 通常无法反序列化该事件（registry 里没注册），因此更依赖 **Writer-second**：

- **先部署能处理新事件类型的版本**（包含 registry + handler + 投影更新）。
- **再开启写入新事件类型**，建议用特性开关控制。

字段变更可以靠 upcaster 打补丁，但新事件类型的引入基本没有"读端事后兼容"的余地，必须严格顺序。

## 4. 回放 fail-fast

默认情况下，`app/eventsourced.DomainEventStore.RestoreAggregate` 在回放阶段对"自动路由 handler"采用 fail-fast 语义：

> 若某个事件未命中任何 handler，返回错误。

这样做是为了避免聚合状态静默漂移——如果某个事件在当前版本已经没人处理，聚合重建后的状态就不再等于当初写下的状态，这是事件溯源里最危险的一种 bug。

当前版本**不提供**"忽略未命中 handler"的回放路径。如果你在回放中遇到未命中 handler 的事件，解决路径只有两个：补齐 handler，或者引入 upcaster / 迁移脚本处理根因。
