// Package outbox 实现 Outbox Pattern，确保事件发布的可靠性。
//
// Outbox Pattern 解决的问题：
// 1. 聚合保存和事件发布的原子性。
// 2. 事件发布失败时的重试机制。
// 3. 分布式系统中的最终一致性保证。
//
// 本包的 DLQ 处理“事件尚未成功发布”且持久重试耗尽的 Outbox 记录，保留查询与重投语义。
// 消息已经投递给 handler 后发生的处理失败属于 messaging/deadletter.ISink 的职责；两者处于
// 发布链路的不同阶段，不共享状态契约。
package outbox
