package middleware

import (
	"context"
	"sort"
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

// lockEntry 锁条目，包含锁和最后使用时间。
type lockEntry struct {
	mutex    *sync.Mutex
	lastUsed time.Time
	// active 表示当前正在使用或等待该锁的 goroutine 数。
	// 只有 active==0 的锁才允许被清理/淘汰，避免出现同一聚合对应多把锁的并发破坏。
	active int
}

type typeLockEntry struct {
	mutex  *sync.Mutex
	active int
}

// aggregateLockHandle 聚合锁句柄。
//
// 用于在不暴露内部计数的前提下，安全地在 Unlock 时回收 active 计数。
type aggregateLockHandle struct {
	aggregateID string
	entry       *lockEntry
	middleware  *AggregateLockMiddleware
}

// Lock 提供middleware能力。
func (h *aggregateLockHandle) Lock() {
	h.entry.mutex.Lock()
}

// Unlock 提供middleware能力。
func (h *aggregateLockHandle) Unlock() {
	h.entry.mutex.Unlock()
	h.middleware.releaseAggregateLock(h.aggregateID)
}

type typeLockHandle struct {
	aggregateType string
	entry         *typeLockEntry
	middleware    *AggregateLockMiddleware
}

func (h *typeLockHandle) Lock() {
	h.entry.mutex.Lock()
}

func (h *typeLockHandle) Unlock() {
	h.entry.mutex.Unlock()
	h.middleware.releaseTypeLock(h.aggregateType)
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
	if config.Clock == nil {
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
		handle.Lock()
		defer handle.Unlock()
	default: // "aggregate" 或其他值均视为按聚合 ID 加锁
		if aggregateID == "" {
			// 没有聚合 ID，不需要加锁
			return next(ctx, message)
		}
		handle := m.getOrCreateAggregateLockHandle(aggregateID)
		handle.Lock()
		defer handle.Unlock()
	}

	// 执行命令
	return next(ctx, message)
}

func (m *AggregateLockMiddleware) Name() string {
	return "CommandAggregateLock"
}

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

// Clear 清空所有已缓存的空闲锁映射，主要用于测试重置。
func (m *AggregateLockMiddleware) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for id, entry := range m.locksByAggregate {
		if entry == nil || entry.active <= 0 {
			delete(m.locksByAggregate, id)
		}
	}
	for aggregateType, entry := range m.locksByType {
		if entry == nil || entry.active <= 0 {
			delete(m.locksByType, aggregateType)
		}
	}
}

// Prune 清理当前所有空闲（无活跃持有者）的锁映射，等价于 Clear。
// 正在被持有的锁（active > 0）不会被移除，以保证并发安全。
func (m *AggregateLockMiddleware) Prune() {
	m.Clear()
}

// PruneExpired 删除所有空闲超过阈值的聚合锁。
func (m *AggregateLockMiddleware) PruneExpired() int {
	if m.lockIdleTimeout <= 0 {
		return 0
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.cleanupExpiredLocks()
}

// cleanupExpiredLocks 删除空闲超时且当前无活跃持有者的锁；调用者需持有写锁。
func (m *AggregateLockMiddleware) cleanupExpiredLocks() int {
	if m.lockIdleTimeout <= 0 {
		return 0
	}

	now := m.clock.Now()
	cutoff := now.Add(-m.lockIdleTimeout)
	pruned := 0

	for id, entry := range m.locksByAggregate {
		if entry.active == 0 && entry.lastUsed.Before(cutoff) {
			delete(m.locksByAggregate, id)
			pruned++
		}
	}

	return pruned
}

// evictOldestLocks 淘汰最久未使用的锁，使锁数量回到上限之内。
func (m *AggregateLockMiddleware) evictOldestLocks() {
	if m.maxAggregateLocks <= 0 {
		return
	}

	// 如果未超过限制，无需淘汰
	if len(m.locksByAggregate) < m.maxAggregateLocks {
		return
	}

	type item struct {
		id string
		t  time.Time
	}
	items := make([]item, 0, len(m.locksByAggregate))
	for id, entry := range m.locksByAggregate {
		items = append(items, item{id: id, t: entry.lastUsed})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].t.Before(items[j].t)
	})

	overflow := len(m.locksByAggregate) - m.maxAggregateLocks + 1
	if overflow <= 0 {
		return
	}

	for i := 0; i < overflow && i < len(items); i++ {
		id := items[i].id
		entry := m.locksByAggregate[id]
		if entry == nil {
			delete(m.locksByAggregate, id)
			continue
		}
		if entry.active != 0 {
			continue
		}
		delete(m.locksByAggregate, id)
	}
}

// getOrCreateAggregateLockHandle 获取或创建聚合锁句柄。
func (m *AggregateLockMiddleware) getOrCreateAggregateLockHandle(aggregateID string) *aggregateLockHandle {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 先清理过期锁
	m.cleanupExpiredLocks()

	// 超过限制则淘汰最旧锁
	if m.maxAggregateLocks > 0 && len(m.locksByAggregate) >= m.maxAggregateLocks {
		m.evictOldestLocks()
	}

	entry, exists := m.locksByAggregate[aggregateID]
	if !exists {
		now := m.clock.Now()
		entry = &lockEntry{
			mutex:    &sync.Mutex{},
			lastUsed: now,
			active:   0,
		}
		m.locksByAggregate[aggregateID] = entry
	}

	// 增加活跃计数并更新时间戳
	entry.active++
	entry.lastUsed = m.clock.Now()

	return &aggregateLockHandle{
		aggregateID: aggregateID,
		entry:       entry,
		middleware:  m,
	}
}

// releaseAggregateLock 释放聚合锁引用计数。
func (m *AggregateLockMiddleware) releaseAggregateLock(aggregateID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	entry, ok := m.locksByAggregate[aggregateID]
	if !ok || entry == nil {
		return
	}
	if entry.active > 0 {
		entry.active--
	}
	entry.lastUsed = m.clock.Now()
}

// getOrCreateTypeLockHandle 获取或创建类型锁句柄。
func (m *AggregateLockMiddleware) getOrCreateTypeLockHandle(aggregateType string) *typeLockHandle {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	entry, ok := m.locksByType[aggregateType]
	if !ok {
		entry = &typeLockEntry{mutex: &sync.Mutex{}}
		m.locksByType[aggregateType] = entry
	}
	entry.active++

	return &typeLockHandle{
		aggregateType: aggregateType,
		entry:         entry,
		middleware:    m,
	}
}

func (m *AggregateLockMiddleware) releaseTypeLock(aggregateType string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	entry, ok := m.locksByType[aggregateType]
	if !ok || entry == nil {
		return
	}
	if entry.active > 0 {
		entry.active--
	}
}
