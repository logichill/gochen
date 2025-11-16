package middleware

import (
	"context"
	"sync"

	"gochen/messaging"
	"gochen/messaging/command"
)

// AggregateLockMiddleware 聚合级锁中间件
//
// 确保针对同一聚合的命令串行执行，防止并发冲突。
// 这是 CommandBus 内置聚合锁的中间件版本，可单独使用。
//
// 使用场景：
//   - 防止同一聚合的并发写入冲突
//   - 简化应用层并发控制
//   - 乐观锁的替代方案
//
// 注意：
//   - 只在单进程内有效
//   - 分布式环境需要使用分布式锁（Redis、Etcd 等）
//   - 可能影响吞吐量（串行执行）
type AggregateLockMiddleware struct {
	// locksByAggregate 聚合 ID 到锁的映射（LockGranularity = "aggregate" 时使用）
	locksByAggregate map[int64]*sync.Mutex

	// locksByType 聚合类型到锁的映射（LockGranularity = "type" 时使用）
	locksByType map[string]*sync.Mutex

	mutex sync.RWMutex

	// lockGranularity 锁粒度
	// "aggregate": 每个聚合独立锁
	// "type": 每个聚合类型共享锁
	lockGranularity string
}

// AggregateLockConfig 聚合锁配置
type AggregateLockConfig struct {
	// LockGranularity 锁粒度
	// "aggregate": 每个聚合 ID 独立锁（默认）
	// "type": 每个聚合类型共享一个锁
	LockGranularity string
}

// DefaultAggregateLockConfig 默认配置
func DefaultAggregateLockConfig() *AggregateLockConfig {
	return &AggregateLockConfig{
		LockGranularity: "aggregate",
	}
}

// NewAggregateLockMiddleware 创建聚合锁中间件
//
// 参数：
//   - config: 配置，nil 则使用默认配置
//
// 返回：
//   - *AggregateLockMiddleware: 中间件实例
func NewAggregateLockMiddleware(config *AggregateLockConfig) *AggregateLockMiddleware {
	if config == nil {
		config = DefaultAggregateLockConfig()
	}

	return &AggregateLockMiddleware{
		locksByAggregate: make(map[int64]*sync.Mutex),
		locksByType:      make(map[string]*sync.Mutex),
		lockGranularity:  config.LockGranularity,
	}
}

// Handle 实现 messaging.IMiddleware 接口
//
// 执行流程：
//  1. 检查消息类型，只处理命令
//  2. 获取聚合 ID
//  3. 获取对应的锁
//  4. 加锁
//  5. 执行命令
//  6. 解锁
func (m *AggregateLockMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	// 只处理命令消息
	if message.GetType() != messaging.MessageTypeCommand {
		return next(ctx, message)
	}

	// 类型断言为 Command
	cmd, ok := message.(*command.Command)
	if !ok {
		return next(ctx, message)
	}

	// 获取聚合 ID
	aggregateID := cmd.GetAggregateID()
	aggregateType := cmd.GetAggregateType()

	// 选择锁粒度
	var lock *sync.Mutex
	switch m.lockGranularity {
	case "type":
		// 按聚合类型加锁；没有类型信息则直接透传
		if aggregateType == "" {
			return next(ctx, message)
		}
		lock = m.getOrCreateTypeLock(aggregateType)
	default: // "aggregate" 或其他值均视为按聚合 ID 加锁
		if aggregateID == 0 {
			// 没有聚合 ID，不需要加锁
			return next(ctx, message)
		}
		lock = m.getOrCreateAggregateLock(aggregateID)
	}

	// 加锁
	lock.Lock()
	defer lock.Unlock()

	// 执行命令
	return next(ctx, message)
}

// Name 实现 messaging.IMiddleware 接口
func (m *AggregateLockMiddleware) Name() string {
	return "CommandAggregateLock"
}

// GetLockCount 获取当前锁的数量（用于监控）
func (m *AggregateLockMiddleware) GetLockCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	switch m.lockGranularity {
	case "type":
		return len(m.locksByType)
	default:
		return len(m.locksByAggregate)
	}
}

// Clear 清空所有锁（用于测试）
func (m *AggregateLockMiddleware) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.locksByAggregate = make(map[int64]*sync.Mutex)
	m.locksByType = make(map[string]*sync.Mutex)
}

// Prune 清理所有当前锁映射（用于维护窗口）
//
// 注意：
//   - 该方法不会尝试检测锁是否正在使用；
//   - 调用方应在确认没有正在执行的命令（或可以接受少量竞争失败）的前提下调用；
//   - 在高并发、长时间运行场景下，可在运维窗口周期性调用以释放不再需要的锁映射。
func (m *AggregateLockMiddleware) Prune() {
	m.Clear()
}

// getOrCreateAggregateLock 获取或创建按聚合 ID 的锁
func (m *AggregateLockMiddleware) getOrCreateAggregateLock(aggregateID int64) *sync.Mutex {
	// 快速路径：读锁检查
	m.mutex.RLock()
	lock, exists := m.locksByAggregate[aggregateID]
	m.mutex.RUnlock()

	if exists {
		return lock
	}

	// 慢速路径：创建新锁
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 双重检查
	if lock, exists := m.locksByAggregate[aggregateID]; exists {
		return lock
	}

	lock = &sync.Mutex{}
	m.locksByAggregate[aggregateID] = lock
	return lock
}

// getOrCreateTypeLock 获取或创建按聚合类型的锁
func (m *AggregateLockMiddleware) getOrCreateTypeLock(aggregateType string) *sync.Mutex {
	// 快速路径：读锁检查
	m.mutex.RLock()
	lock, exists := m.locksByType[aggregateType]
	m.mutex.RUnlock()

	if exists {
		return lock
	}

	// 慢速路径：创建新锁
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 双重检查
	if lock, exists := m.locksByType[aggregateType]; exists {
		return lock
	}

	lock = &sync.Mutex{}
	m.locksByType[aggregateType] = lock
	return lock
}
