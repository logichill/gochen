package governance

import (
	"gochen/clock"
	"gochen/gen"
)

// Option 配置治理外壳。
type Option func(*config)

type config struct {
	snapshot    IPolicySnapshot
	auditStore  IAuthzAuditStore
	metrics     IAuthzMetrics
	clock       clock.IClock
	idGenerator gen.IGenerator[string]

	// strictAudit 为 true 时审计写入失败会阻塞授权（默认 false）。
	strictAudit bool
	// lenientSnapshot 为 true 时快照解析失败不阻塞授权（默认 false）。
	lenientSnapshot bool
}

// WithSnapshot 启用策略快照解析及版本留痕。
// 解析版本写入 Decision.SnapshotVersion；实际评估对该版本的绑定由策略实现负责。
//
// 失败策略：默认 **fail-closed**——快照获取失败直接拒绝请求。
// 可用 WithLenientSnapshot 覆盖为宽松模式。
func WithSnapshot(snapshot IPolicySnapshot) Option {
	return func(c *config) { c.snapshot = snapshot }
}

// WithAudit 启用决策审计留痕。
//
// 失败策略：默认 **fail-open**——审计写入失败不阻塞授权。
// 可靠重试与告警由 store 或外部队列负责；可用 WithStrictAudit 改为阻塞授权。
func WithAudit(store IAuthzAuditStore) Option {
	return func(c *config) { c.auditStore = store }
}

// WithMetrics 启用决策指标上报。
//
// 失败策略：静默降级，永不影响授权。
func WithMetrics(metrics IAuthzMetrics) Option {
	return func(c *config) { c.metrics = metrics }
}

// WithStrictAudit 把审计失败改为阻塞授权（合规严格模式）。
func WithStrictAudit() Option {
	return func(c *config) { c.strictAudit = true }
}

// WithLenientSnapshot 把快照解析失败改为不阻塞授权。
//
// 解析失败时继续执行底层策略，此次决策缺少解析器提供的版本。
func WithLenientSnapshot() Option {
	return func(c *config) { c.lenientSnapshot = true }
}

// WithClock 注入时钟（便于测试确定性）。
func WithClock(c clock.IClock) Option {
	return func(cfg *config) { cfg.clock = c }
}

// WithIDGenerator 注入决策 ID 生成器。
func WithIDGenerator(g gen.IGenerator[string]) Option {
	return func(c *config) { c.idGenerator = g }
}
