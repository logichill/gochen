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

// DefaultOutboxConfig 返回开箱即用的发件箱默认配置。
func DefaultOutboxConfig() OutboxConfig {
	return OutboxConfig{
		PublishInterval:       5 * time.Second,
		BatchSize:             100,
		MaxRetries:            5,
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
	if cfg.MaxRetries <= 0 {
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

func normalizeOutboxConfigForRepository[ID comparable](cfg OutboxConfig, repo IOutboxRepository[ID]) (OutboxConfig, error) {
	requestedLease := cfg.ClaimLease
	requestedRenewInterval := cfg.ClaimRenewInterval
	cfg = NormalizeOutboxConfig(cfg)

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
