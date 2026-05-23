package cached

import "time"

// Config 缓存配置，用于控制事件存储的缓存行为（TTL、最大聚合数、清理间隔）。
type Config struct {
	TTL             time.Duration // 缓存过期时间（默认: 5分钟）
	MaxAggregates   int           // 最大缓存聚合数（默认: 1000）
	CleanupInterval time.Duration // 清理间隔（默认: 1分钟）
}

const defaultMaxAggregates = 1000

// DefaultConfig 默认缓存配置。
func DefaultConfig() *Config {
	return &Config{
		TTL:             5 * time.Minute,
		MaxAggregates:   defaultMaxAggregates,
		CleanupInterval: 1 * time.Minute,
	}
}
