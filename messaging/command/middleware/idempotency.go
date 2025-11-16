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

	// ttl 命令 ID 的过期时间
	// 超过此时间的记录会被清理，允许重新执行
	ttl time.Duration

	// cleanupInterval 清理间隔
	cleanupInterval time.Duration

	// stopCleanup 停止清理的信号
	stopCleanup chan struct{}
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

	// 严格串行化检查与记录，避免并发下重复执行
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 已处理且未过期：直接返回成功（幂等行为）
	if processedAt, exists := m.processed[commandID]; exists {
		if time.Since(processedAt) <= m.ttl {
			return nil
		}
	}

	// 未处理或已过期：执行命令
	err := next(ctx, message)

	// 只有成功时才记录（失败的命令可以重试）
	if err == nil {
		m.processed[commandID] = time.Now()
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
func (m *IdempotencyMiddleware) Stop() {
	close(m.stopCleanup)
}

// Clear 清空所有记录（用于测试）
func (m *IdempotencyMiddleware) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.processed = make(map[string]time.Time)
}

// GetProcessedCount 获取已处理命令数（用于监控）
func (m *IdempotencyMiddleware) GetProcessedCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return len(m.processed)
}
