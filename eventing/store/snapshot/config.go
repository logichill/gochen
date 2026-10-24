package snapshot

import (
	"time"
)

// Config 快照配置，用于控制事件快照的生成和存储策略。
type Config struct {
	Frequency       int           `json:"frequency"`
	RetentionPeriod time.Duration `json:"retention_period"`
	MaxSnapshots    int           `json:"max_snapshots"`
	Enabled         bool          `json:"enabled"`
}

// DefaultConfig 返回快照管理器的默认配置。
func DefaultConfig() *Config {
	return &Config{Frequency: 100, RetentionPeriod: 30 * 24 * time.Hour, MaxSnapshots: 10, Enabled: true}
}

func normalizeConfig(config *Config) *Config {
	defaults := DefaultConfig()
	if config == nil {
		return defaults
	}
	normalized := *config
	if normalized.RetentionPeriod <= 0 {
		normalized.RetentionPeriod = defaults.RetentionPeriod
	}
	return &normalized
}
