package cache

import (
	"time"

	"gochen/clock"
)

// Config 缓存配置，用于控制存储层缓存的行为（TTL、最大条目数、清理策略等）。
type Config struct {
	// Name 缓存名称（用于日志和统计）
	Name string

	// MaxSize 最大缓存条目数，0 表示无限制（不推荐）
	MaxSize int

	// TTL 缓存过期时间，基于访问时间
	// 0 表示永不过期
	TTL time.Duration

	// Clock 可选：用于 Now()/Since() 等时间来源；便于测试稳定推进时间。
	Clock clock.IClock

	// EnableStats 是否启用统计（默认启用）。
	//
	// 说明：
	// - nil 表示启用（默认）；
	// - false 表示关闭统计（Hits/Misses/Evictions/Expires 始终为 0）。
	EnableStats *bool

	// OnEvict 驱逐回调（可选）。
	//
	// 说明：
	// - 回调在内部锁外执行，可重入访问缓存；
	// - 若回调 panic，本次批量驱逐的后续回调仍会继续执行；全部回调结束后会重新抛出首个 panic。
	// 使用 any 以支持不同的键值类型。
	OnEvict func(key, value any)

	// CleanupInterval 后台主动清理已过期条目的扫描间隔。
	//
	// 0 或负值表示不开启后台清理（默认，保持既有行为）。
	// 仅对 TTL > 0 生效：被 Get 命中的过期条目仍会在 Get 时惰性移除，
	// 但从未被再次访问的已过期条目会永久驻留（MaxSize==0 时无上限地增长）。
	// 需要长期运行的服务应设置该值（或自行周期性调用 CleanExpired）。
	CleanupInterval time.Duration
}
