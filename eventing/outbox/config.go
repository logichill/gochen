package outbox

import (
	"time"

	"gochen/errors"
)

const defaultClaimLease = 5 * time.Minute

// OutboxConfig 定义发件箱配置。
type OutboxConfig struct {
	// 发布间隔
	PublishInterval time.Duration `json:"publish_interval"`

	// 每次处理的最大记录数
	BatchSize int `json:"batch_size"`

	// 最大重试次数
	MaxRetries int `json:"max_retries"`

	// 重试间隔（指数退避）
	RetryInterval time.Duration `json:"retry_interval"`

	// 历史数据清理间隔
	CleanupInterval time.Duration `json:"cleanup_interval"`

	// 保留已发布记录的时间
	RetentionPeriod time.Duration `json:"retention_period"`

	// ClaimLease 是单条记录被 publisher claim 后的租约时长。
	ClaimLease time.Duration `json:"claim_lease"`

	// ClaimRenewInterval 是发布过程中续约 claim 的间隔；为 0 时默认使用 ClaimLease 的一半。
	ClaimRenewInterval time.Duration `json:"claim_renew_interval"`

	// MarkPublishedAttempts 是事件已成功发布后写回 published 状态的总尝试次数。
	MarkPublishedAttempts int `json:"mark_published_attempts"`
}

// DefaultMaxRetries 发件箱默认最大重试次数。
const DefaultMaxRetries = 5

// NoMaxRetries 表示禁用最大重试上限（无限重试，直到成功或人工介入）。
const NoMaxRetries = -1

// DefaultOutboxConfig 返回开箱即用的发件箱默认配置。
func DefaultOutboxConfig() OutboxConfig {
	return OutboxConfig{
		PublishInterval:       5 * time.Second,
		BatchSize:             100,
		MaxRetries:            DefaultMaxRetries,
		RetryInterval:         30 * time.Second,
		CleanupInterval:       1 * time.Hour,
		RetentionPeriod:       7 * 24 * time.Hour, // 保留 7 天
		ClaimLease:            defaultClaimLease,
		MarkPublishedAttempts: 3,
	}
}

// NormalizeOutboxConfig 为零值字段填充默认值，并约束 ClaimRenewInterval 必须小于 ClaimLease。
// 导出以供 sqlstore 等子包复用，避免归一化逻辑重复。
func NormalizeOutboxConfig(cfg OutboxConfig) OutboxConfig {
	defaults := DefaultOutboxConfig()
	if cfg.PublishInterval <= 0 {
		cfg.PublishInterval = defaults.PublishInterval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaults.BatchSize
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = defaults.MaxRetries
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = defaults.RetryInterval
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = defaults.CleanupInterval
	}
	if cfg.RetentionPeriod <= 0 {
		cfg.RetentionPeriod = defaults.RetentionPeriod
	}
	if cfg.ClaimLease <= 0 {
		cfg.ClaimLease = defaults.ClaimLease
	}
	if cfg.ClaimRenewInterval <= 0 || cfg.ClaimRenewInterval >= cfg.ClaimLease {
		cfg.ClaimRenewInterval = cfg.ClaimLease / 2
	}
	if cfg.ClaimRenewInterval <= 0 {
		cfg.ClaimRenewInterval = time.Second
	}
	if cfg.MarkPublishedAttempts <= 0 {
		cfg.MarkPublishedAttempts = defaults.MarkPublishedAttempts
	}
	return cfg
}

type claimLeaseProvider interface {
	GetClaimLease() time.Duration
}

// IOutboxMaxRetriesAware 供 Outbox 仓储实现声明并与 publisher 协商 MaxRetries 语义。
//
// MaxRetries 在管道中有三个消费点，必须同源对齐：仓储的 claim 门禁（超限不再
// claim）、publisher 的 DLQ 迁移判定（超限迁死信）、cleanup 的清理阈值。核心
// 在 NewPublisher / NewParallelPublisher 构造时通过本接口与仓储协商出同一个
// 上限：仓储显式配置优先，否则把 publisher 配置下发到仓储。若仓储不实现本
// 接口（如仅内存测试用的简单实现），协商被跳过，claim 门禁与 DLQ 判定可能
// 分叉——外部仓储实现（如 SQL 仓储）应实现本接口并参与一致性校验。
type IOutboxMaxRetriesAware interface {
	// ConfiguredMaxRetries 返回仓储显式配置的重试上限；configured 为 false
	// 表示仓储未显式配置（含被 ApplyMaxRetries 下发前），此时 publisher 会
	// 用自身配置下发（见 ApplyMaxRetries）。
	ConfiguredMaxRetries() (maxRetries int, configured bool)

	// ApplyMaxRetries 把 publisher 侧解析后的重试上限同步到仓储，使 claim
	// 门禁与 publisher 的 DLQ 判定共享同一阈值。实现方应把首次下发视为已
	// 完成对齐：此后 ConfiguredMaxRetries 应报告 configured=true，避免后续
	// 构造的其他 publisher 用不同配置覆盖对齐结果。
	ApplyMaxRetries(maxRetries int)
}

// maxRetriesAdapter 是 IOutboxMaxRetriesAware 的历史内部别名。
type maxRetriesAdapter = IOutboxMaxRetriesAware

func normalizeOutboxConfigForRepository[ID comparable](cfg OutboxConfig, repo IOutboxRepository[ID]) (OutboxConfig, error) {
	requestedLease := cfg.ClaimLease
	requestedRenewInterval := cfg.ClaimRenewInterval
	cfg = NormalizeOutboxConfig(cfg)

	if adapter, ok := any(repo).(maxRetriesAdapter); ok {
		// 仓储显式配置优先（显式意图不应被构造参数覆盖），否则采用 publisher 配置。
		if repoMaxRetries, configured := adapter.ConfiguredMaxRetries(); configured {
			cfg.MaxRetries = repoMaxRetries
		} else {
			adapter.ApplyMaxRetries(cfg.MaxRetries)
		}
	}

	provider, ok := any(repo).(claimLeaseProvider)
	if !ok {
		return cfg, nil
	}
	repoLease := provider.GetClaimLease()
	if repoLease <= 0 {
		return cfg, nil
	}
	if requestedLease > 0 && requestedLease != repoLease {
		return OutboxConfig{}, errors.NewCode(errors.InvalidInput, "outbox claim lease mismatch between publisher and repository").
			WithContext("publisher_claim_lease", requestedLease.String()).
			WithContext("repository_claim_lease", repoLease.String())
	}

	cfg.ClaimLease = repoLease
	if requestedRenewInterval <= 0 || requestedRenewInterval >= cfg.ClaimLease {
		cfg.ClaimRenewInterval = cfg.ClaimLease / 2
		if cfg.ClaimRenewInterval <= 0 {
			cfg.ClaimRenewInterval = time.Second
		}
	}
	return cfg, nil
}
