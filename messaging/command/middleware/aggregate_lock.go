package middleware

import (
	"context"
	"sync"
	"time"

	"gochen/clock"
	"gochen/messaging"
	"gochen/messaging/command"
)

// AggregateLockMiddleware 聚合级锁中间件。
//
// 确保针对同一聚合的命令串行执行，防止并发冲突。
// 该中间件可挂到 CommandBus（投递侧）或 CommandExecutor（执行侧）独立复用。
//
// 使用场景：
//   - 防止同一聚合的并发写入冲突。
//   - 简化应用层并发控制。
//   - 乐观锁的替代方案。
//
// 注意：
//   - 只在单进程内有效。
//   - 分布式环境需要使用分布式锁（Redis、Etcd 等）
//   - 可能影响吞吐量（串行执行）
type AggregateLockMiddleware struct {
	// locksByAggregate 聚合 ID 到锁的映射（LockGranularity = "aggregate" 时使用）
	locksByAggregate map[string]*lockEntry

	// locksByType 聚合类型到锁的映射（LockGranularity = "type" 时使用）
	locksByType map[string]*typeLockEntry

	mutex sync.RWMutex

	// lockGranularity 锁粒度
	// "aggregate": 每个聚合独立锁
	// "type": 每个聚合类型共享锁
	lockGranularity string

	// maxAggregateLocks 最大聚合锁数量（仅 aggregate 粒度有效）
	// 超过此数量时会淘汰最久未使用的锁
	// 0 表示无限制（不推荐）
	maxAggregateLocks int

	// lockIdleTimeout 锁空闲超时时间
	// 超过此时间未使用的锁会被清理
	// 0 表示不基于时间清理
	lockIdleTimeout time.Duration

	// clock 用于 lastUsed/idle/eviction 的时间来源（便于测试稳定控制）。
	clock clock.IClock
}

// AggregateLockConfig 聚合锁配置。
type AggregateLockConfig struct {
	// LockGranularity 锁粒度
	// "aggregate": 每个聚合 ID 独立锁（默认）
	// "type": 每个聚合类型共享一个锁
	LockGranularity string

	// MaxAggregateLocks 最大聚合锁数量（仅 aggregate 粒度有效）
	// 超过此数量时会淘汰最久未使用的锁
	// 默认值 10000，设置为 0 表示无限制（不推荐，可能导致内存泄漏）
	MaxAggregateLocks int

	// LockIdleTimeout 锁空闲超时时间
	// 超过此时间未使用的锁会在下次访问时被清理
	// 默认值 30 分钟，设置为 0 表示不基于时间清理
	LockIdleTimeout time.Duration

	// Clock 可选：用于内部 lastUsed/idle/eviction 的时间来源，便于测试稳定推进时间。
	Clock clock.IClock
}

func DefaultAggregateLockConfig() *AggregateLockConfig {
	return &AggregateLockConfig{
		LockGranularity:   "aggregate",
		MaxAggregateLocks: 10000,
		LockIdleTimeout:   30 * time.Minute,
		Clock:             clock.NewRealClock(),
	}
}

// NewAggregateLockMiddleware 创建一个按聚合或聚合类型串行化命令的中间件。
func NewAggregateLockMiddleware(config *AggregateLockConfig) *AggregateLockMiddleware {
	if config == nil {
		config = DefaultAggregateLockConfig()
	}
	if isNilClock(config.Clock) {
		config.Clock = clock.NewRealClock()
	}

	return &AggregateLockMiddleware{
		locksByAggregate:  make(map[string]*lockEntry),
		locksByType:       make(map[string]*typeLockEntry),
		lockGranularity:   config.LockGranularity,
		maxAggregateLocks: config.MaxAggregateLocks,
		lockIdleTimeout:   config.LockIdleTimeout,
		clock:             config.Clock,
	}
}

// Handle 仅对命令消息加锁，确保同一聚合上的命令不会并发执行。
func (m *AggregateLockMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	// 防空保护：nil 或 typed-nil 消息直接调用 GetKind 会 panic
	if messaging.IsNilMessage(message) {
		return next(ctx, message)
	}
	// 只处理命令消息
	if message.GetKind() != messaging.KindCommand {
		return next(ctx, message)
	}

	// 类型断言为 *command.Command，提取聚合信息。
	// 注意：该中间件依赖 *command.Command 的具体类型来获取 AggregateID/AggregateType。
	// 若消息实现了 KindCommand 但不是 *command.Command（例如自定义命令类型），
	// 则跳过加锁直接透传——调用方需确保自定义命令类型也使用 *command.Command 或为其单独配置中间件。
	cmd, ok := message.(*command.Command)
	if !ok {
		return next(ctx, message)
	}

	// 获取聚合 ID
	aggregateID := cmd.GetAggregateID()
	aggregateType := cmd.GetAggregateType()

	// 选择锁粒度
	switch m.lockGranularity {
	case "type":
		// 按聚合类型加锁；没有类型信息则直接透传
		if aggregateType == "" {
			return next(ctx, message)
		}
		handle := m.getOrCreateTypeLockHandle(aggregateType)
		if err := handle.Lock(ctx); err != nil {
			return err
		}
		defer handle.Unlock()
	default: // "aggregate" 或其他值均视为按聚合 ID 加锁
		if aggregateID == "" {
			// 没有聚合 ID，不需要加锁
			return next(ctx, message)
		}
		handle := m.getOrCreateAggregateLockHandle(aggregateID)
		if err := handle.Lock(ctx); err != nil {
			return err
		}
		defer handle.Unlock()
	}

	// 执行命令
	return next(ctx, message)
}

func (m *AggregateLockMiddleware) Name() string {
	return "CommandAggregateLock"
}
