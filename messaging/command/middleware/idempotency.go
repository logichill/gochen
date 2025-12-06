package middleware

import (
	"context"
	"sync"
	"time"

	"gochen/messaging"
)

// IdempotencyMiddleware 幂等性中间件
//
// 基于命令 ID 确保命令的幂等性，防止重复执行。
// 使用内存存储已处理的命令 ID（生产环境建议使用 Redis 等持久化存储）。
//
// 特性：
//   - 基于命令 ID 去重
//   - 可配置 TTL（过期时间）
//   - 线程安全
//   - 只对命令类型消息生效
type IdempotencyMiddleware struct {
	// processed 已处理的命令 ID 及其处理时间
	processed map[string]time.Time
	mutex     sync.RWMutex

	// locks 对每个命令 ID 维护一把细粒度锁
	// 保证同一 ID 的命令串行执行，不同 ID 可以并发
	locks map[string]*sync.Mutex

	// ttl 命令 ID 的过期时间
	// 超过此时间的记录会被清理，允许重新执行
	ttl time.Duration

	// cleanupInterval 清理间隔
	cleanupInterval time.Duration

	// stopCleanup 停止清理的信号
	stopCleanup chan struct{}

	// stopOnce 确保 Stop() 只执行一次，避免重复关闭 channel
	stopOnce sync.Once
}

// IdempotencyConfig 幂等性配置
type IdempotencyConfig struct {
	// TTL 命令 ID 过期时间（默认：1小时）
	TTL time.Duration

	// CleanupInterval 清理间隔（默认：10分钟）
	CleanupInterval time.Duration
}

// DefaultIdempotencyConfig 默认配置
func DefaultIdempotencyConfig() *IdempotencyConfig {
	return &IdempotencyConfig{
		TTL:             time.Hour,
		CleanupInterval: 10 * time.Minute,
	}
}

// NewIdempotencyMiddleware 创建幂等性中间件
//
// 参数：
//   - config: 配置，nil 则使用默认配置
//
// 返回：
//   - *IdempotencyMiddleware: 中间件实例
func NewIdempotencyMiddleware(config *IdempotencyConfig) *IdempotencyMiddleware {
	if config == nil {
		config = DefaultIdempotencyConfig()
	}

	m := &IdempotencyMiddleware{
		processed:       make(map[string]time.Time),
		locks:           make(map[string]*sync.Mutex),
		ttl:             config.TTL,
		cleanupInterval: config.CleanupInterval,
		stopCleanup:     make(chan struct{}),
	}

	// 启动后台清理 goroutine
	go m.startCleanupWorker()

	return m
}

// Handle 实现 messaging.IMiddleware 接口
//
// 执行流程：
//  1. 检查消息类型，只处理命令
//  2. 获取命令 ID
//  3. 检查命令是否已处理
//  4. 如果已处理，直接返回成功（幂等）
//  5. 如果未处理，执行命令并记录
func (m *IdempotencyMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	// 只处理命令消息
	if message.GetType() != messaging.MessageTypeCommand {
		return next(ctx, message)
	}

	commandID := message.GetID()
	if commandID == "" {
		// 没有 ID，无法做幂等性检查，直接执行
		return next(ctx, message)
	}

	// 按命令 ID 串行化检查与记录，避免并发下重复执行
	lock := m.getOrCreateLock(commandID)
	lock.Lock()
	defer func() {
		lock.Unlock()
		m.releaseLock(commandID, lock)
	}()

	// 已处理且未过期：直接返回成功（幂等行为）
	if m.isProcessed(commandID) {
		return nil
	}

	// 未处理或已过期：执行命令
	err := next(ctx, message)

	// 只有成功时才记录（失败的命令可以重试）
	if err == nil {
		m.markProcessed(commandID)
	}

	return err
}

// Name 实现 messaging.IMiddleware 接口
func (m *IdempotencyMiddleware) Name() string {
	return "CommandIdempotency"
}

// isProcessed 检查命令是否已处理
func (m *IdempotencyMiddleware) isProcessed(commandID string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	processedAt, exists := m.processed[commandID]
	if !exists {
		return false
	}

	// 检查是否过期
	if time.Since(processedAt) > m.ttl {
		return false
	}

	return true
}

// markProcessed 标记命令已处理
func (m *IdempotencyMiddleware) markProcessed(commandID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.processed[commandID] = time.Now()
}

// getOrCreateLock 获取或创建指定命令 ID 的细粒度锁
func (m *IdempotencyMiddleware) getOrCreateLock(commandID string) *sync.Mutex {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if lock, ok := m.locks[commandID]; ok {
		return lock
	}

	lock := &sync.Mutex{}
	m.locks[commandID] = lock
	return lock
}

// releaseLock 在当前调用完成后尝试移除命令 ID 对应的锁
// 仅当映射中的锁仍然是当前这把时才删除，避免并发下误删
func (m *IdempotencyMiddleware) releaseLock(commandID string, lock *sync.Mutex) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if current, ok := m.locks[commandID]; ok && current == lock {
		delete(m.locks, commandID)
	}
}

// startCleanupWorker 启动清理 worker
func (m *IdempotencyMiddleware) startCleanupWorker() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cleanup()
		case <-m.stopCleanup:
			return
		}
	}
}

// cleanup 清理过期的命令记录
func (m *IdempotencyMiddleware) cleanup() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	now := time.Now()
	for commandID, processedAt := range m.processed {
		if now.Sub(processedAt) > m.ttl {
			delete(m.processed, commandID)
		}
	}
}

// Stop 停止清理 worker（用于测试和优雅关闭）
//
// 使用 sync.Once 确保 channel 只被关闭一次，避免多次调用 Stop() 时 panic。
// 这在测试清理或应用优雅关闭时特别重要。
func (m *IdempotencyMiddleware) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCleanup)
	})
}

// Clear 清空所有记录（用于测试）
func (m *IdempotencyMiddleware) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.processed = make(map[string]time.Time)
	m.locks = make(map[string]*sync.Mutex)
}

// GetProcessedCount 获取已处理命令数（用于监控）
func (m *IdempotencyMiddleware) GetProcessedCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return len(m.processed)
}
