package monitoring

import (
	"context"
	"sync"

	"gochen/errors"
)

var (
	defaultRegistryMu sync.RWMutex
	defaultRegistry   *Registry
)

// Registry 提供监控相关的统一入口（指标 + 健康 + 可选扩展快照）。
//
// 使用建议：
// - 业务侧组合根可通过 NewRegistry(...) 自行构造并通过 SetDefaultRegistry 注入全局默认；
// - 框架内部埋点推荐通过显式注入 metrics recorder 落到该 Registry 的 Metrics（避免隐式全局依赖）。
type Registry struct {
	// Metrics 是框架内置的可读内存指标（默认落点）。
	Metrics *Metrics

	// Health 是健康检查注册表（可由业务注册 DB/依赖探测等检查）。
	Health *HealthRegistry

	snapshotStats ISnapshotStatsProvider
	cacheStats    ICacheStatsProvider
	outbox        IOutboxMetricsProvider

	metricsHealthConfig MetricsHealthConfig
}

// Option 用于向监控注册表注入扩展 provider 或覆盖默认配置。
type Option func(*Registry)

// WithSnapshotStatsProvider 为快照管理器接入可观测统计来源。
func WithSnapshotStatsProvider(p ISnapshotStatsProvider) Option {
	return func(r *Registry) { r.snapshotStats = p }
}

// WithCacheStatsProvider 为注册表补充缓存命中率等扩展统计信息。
func WithCacheStatsProvider(p ICacheStatsProvider) Option {
	return func(r *Registry) { r.cacheStats = p }
}

// WithOutboxMetricsProvider 为注册表接入 Outbox 的指标与健康检查。
func WithOutboxMetricsProvider(p IOutboxMetricsProvider) Option {
	return func(r *Registry) { r.outbox = p }
}

// WithMetricsHealthConfig 覆盖内置指标健康检查使用的阈值配置。
func WithMetricsHealthConfig(cfg MetricsHealthConfig) Option {
	return func(r *Registry) { r.metricsHealthConfig = cfg }
}

// NewRegistry 创建注册表。
func NewRegistry(opts ...Option) (*Registry, error) {
	r := &Registry{
		Metrics:             NewMetrics(),
		Health:              NewHealthRegistry(),
		metricsHealthConfig: DefaultMetricsHealthConfig(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	if r.Metrics == nil {
		return nil, errors.NewCode(errors.InvalidInput, "metrics cannot be nil")
	}
	if r.Health == nil {
		return nil, errors.NewCode(errors.InvalidInput, "health registry cannot be nil")
	}

	// 默认注册 Metrics 健康检查：保证开箱即用。
	metricsCheck, err := MetricsHealthCheck(r.Metrics, r.metricsHealthConfig)
	if err != nil {
		return nil, err
	}
	if err := r.Health.Register("eventing.metrics", metricsCheck); err != nil {
		return nil, err
	}

	// 指标装配提示：当指标始终为零时，往往意味着“未注入 recorder”或“尚未产生观测值”。
	// 该检查不阻塞启动，仅作为可观测性提示。
	_ = r.Health.Register("eventing.metrics_wiring", func(ctx context.Context) (HealthStatus, string, error) {
		if r.Metrics == nil {
			return HealthStatusUnhealthy, "metrics recorder is nil", nil
		}
		if r.Metrics.HasNonZeroSnapshot() {
			return HealthStatusHealthy, "metrics recorder is active", nil
		}
		// 这里返回 Healthy：all-zero 可能只是“还没流量”，不应默认影响健康告警语义。
		return HealthStatusHealthy, "metrics snapshot is all-zero (maybe not wired or no activity yet)", nil
	})

	// 如果注入了 outbox provider，则同时注册 outbox 健康检查。
	if r.outbox != nil {
		outboxCheck, err := OutboxHealthCheck(r.outbox)
		if err != nil {
			return nil, err
		}
		if err := r.Health.Register("eventing.outbox", outboxCheck); err != nil {
			return nil, err
		}
	}

	return r, nil
}

// Snapshot 汇总当前指标、健康状态以及已接入的扩展观测信息。
func (r *Registry) Snapshot(ctx context.Context) Snapshot {
	if r == nil {
		r = DefaultRegistry()
	}

	s := Snapshot{
		Timestamp: Now(),
		Metrics:   r.Metrics.Snapshot().Summary(),
		Health:    r.Health.Report(ctx),
	}

	if r.snapshotStats != nil {
		stats, err := r.snapshotStats.SnapshotStats(ctx)
		if err != nil {
			s.SnapshotManager = &SnapshotManagerSnapshot{Error: err.Error()}
		} else {
			s.SnapshotManager = &SnapshotManagerSnapshot{Stats: stats}
		}
	}

	if r.cacheStats != nil {
		s.Cache = &CacheSnapshot{Stats: r.cacheStats.CacheStats()}
	}

	if r.outbox != nil {
		out, err := r.outbox.Snapshot(ctx)
		if err != nil {
			s.Outbox = &OutboxSnapshot{Error: err.Error()}
		} else {
			s.Outbox = out
		}
	}

	return s
}

func DefaultRegistry() *Registry {
	defaultRegistryMu.RLock()
	r := defaultRegistry
	defaultRegistryMu.RUnlock()
	if r != nil {
		return r
	}

	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	if defaultRegistry == nil {
		created, err := NewRegistry()
		if err != nil {
			// DefaultRegistry 用于框架内部埋点，必须始终可用。
			// 初始化失败时回退到“最小可用实例”，并注册一个固定健康检查让 /healthz 明确呈现失败（而不是“看起来 ok”）。
			fallback := &Registry{
				Metrics:             NewMetrics(),
				Health:              NewHealthRegistry(),
				metricsHealthConfig: DefaultMetricsHealthConfig(),
			}
			_ = fallback.Health.Register("eventing.registry_init", func(ctx context.Context) (HealthStatus, string, error) {
				return HealthStatusUnhealthy, "monitoring registry init failed", err
			})
			// 尽量补齐 metrics 健康检查（即使 init 失败，仍让输出结构完整）。
			if check, checkErr := MetricsHealthCheck(fallback.Metrics, fallback.metricsHealthConfig); checkErr == nil {
				_ = fallback.Health.Register("eventing.metrics", check)
			}
			defaultRegistry = fallback
		} else {
			defaultRegistry = created
		}
	}
	return defaultRegistry
}

// SetDefaultRegistry 设置默认注册表。
func SetDefaultRegistry(r *Registry) error {
	if r == nil {
		return errors.NewCode(errors.InvalidInput, "registry cannot be nil")
	}
	if r.Metrics == nil {
		return errors.NewCode(errors.InvalidInput, "metrics cannot be nil")
	}
	if r.Health == nil {
		return errors.NewCode(errors.InvalidInput, "health registry cannot be nil")
	}
	defaultRegistryMu.Lock()
	defaultRegistry = r
	defaultRegistryMu.Unlock()
	return nil
}
