package middleware

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"time"

	"gochen/clock"
	"gochen/errors"
	"gochen/messaging"
	"gochen/messaging/command"
)

const defaultMaxProcessed = 10000

// IdempotencyMiddleware 幂等性中间件。
//
// 基于命令 ID 确保命令的幂等性，防止重复执行。
// 使用内存存储已处理的命令 ID（生产环境建议使用 Redis 等持久化存储）。
//
// 特性：
//   - 基于命令 ID 去重。
//   - 可配置 TTL（过期时间）
//   - 线程安全。
//   - 只对命令类型消息生效。
type IdempotencyMiddleware struct {
	// processed 已处理的命令 ID 及其处理时间
	processed map[string]time.Time
	mutex     sync.RWMutex

	// locks 对每个命令 ID 维护一把细粒度锁
	// 保证同一 ID 的命令串行执行，不同 ID 可以并发
	locks map[string]*idLock

	// ttl 命令 ID 的过期时间
	// 超过此时间的记录会被清理，允许重新执行
	ttl time.Duration

	// cleanupInterval 清理间隔
	cleanupInterval time.Duration

	// maxProcessed 已处理命令的最大缓存数量（超过会按最老优先淘汰）
	maxProcessed int

	// evictWatermark 触发全量排序淘汰的水位线：容量打满后不会每插入一条
	// 都做 O(N log N) 全量排序，而是越过 maxProcessed+~10% 容量才触发一次，
	// 把排序开销摊薄到两次触发之间的所有写操作上。
	evicWatermark int

	// stopCleanup 停止清理的信号
	stopCleanup chan struct{}

	// stopOnce 确保 Stop(ctx) 只执行一次，避免重复关闭 channel
	stopOnce sync.Once

	// clock 用于 TTL/cleanup 的时间来源（便于测试稳定控制）。
	clock clock.IClock
}

// IdempotencyConfig 幂等性配置。
type IdempotencyConfig struct {
	// TTL 命令 ID 过期时间（默认：1小时）
	TTL time.Duration

	// CleanupInterval 清理间隔（默认：10分钟）
	CleanupInterval time.Duration

	// MaxProcessed 已处理命令最大缓存数量（默认：10000）
	//
	// 说明：
	// - 用于避免在高吞吐场景下仅依赖 TTL 导致的无界增长；
	// - 超过上限会按“最老优先”淘汰，可能导致某些命令在 TTL 内被提前遗忘，从而允许重复执行。
	MaxProcessed int

	// Clock 可选：用于 TTL/cleanup 的时间来源，便于测试稳定控制时间推进。
	Clock clock.IClock
}

func DefaultIdempotencyConfig() *IdempotencyConfig {
	return &IdempotencyConfig{
		TTL:             time.Hour,
		CleanupInterval: 10 * time.Minute,
		MaxProcessed:    defaultMaxProcessed,
		Clock:           clock.NewRealClock(),
	}
}

// isNilClock 判断时钟是否为空，并识别「接口非 nil 而底层指针为 nil」的 typed-nil。
//
// 与 messaging.isNilMessageClock 保持同一约定：typed-nil 时钟按未注入处理，
// 否则中间件会以 nil 接收者调用 Now/NewTicker 触发 panic（清理协程中的 panic 会崩溃整个进程）。
func isNilClock(clk clock.IClock) bool {
	if clk == nil {
		return true
	}
	value := reflect.ValueOf(clk)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// NewIdempotencyMiddleware 创建一个基于命令 ID 去重的内存幂等中间件。
func NewIdempotencyMiddleware(config *IdempotencyConfig) *IdempotencyMiddleware {
	if config == nil {
		config = DefaultIdempotencyConfig()
	}
	if config.TTL <= 0 {
		config.TTL = time.Hour
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 10 * time.Minute
	}
	if config.MaxProcessed <= 0 {
		config.MaxProcessed = defaultMaxProcessed
	}
	if isNilClock(config.Clock) {
		config.Clock = clock.NewRealClock()
	}

	m := &IdempotencyMiddleware{
		processed:       make(map[string]time.Time),
		locks:           make(map[string]*idLock),
		ttl:             config.TTL,
		cleanupInterval: config.CleanupInterval,
		maxProcessed:    config.MaxProcessed,
		evicWatermark:   config.MaxProcessed,
		stopCleanup:     make(chan struct{}),
		clock:           config.Clock,
	}

	// 启动后台清理 goroutine
	go m.startCleanupWorker()

	return m
}

// Handle 仅对命令消息做幂等保护，并保证同一命令 ID 不会被并发重复执行。
// 同一幂等键正在处理时返回 Concurrency，调用方可重试当前操作。
func (m *IdempotencyMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	// 防空保护：nil 或 typed-nil 消息直接调用 GetKind 会 panic
	if messaging.IsNilMessage(message) {
		return next(ctx, message)
	}
	// 只处理命令消息
	if message.GetKind() != messaging.KindCommand {
		return next(ctx, message)
	}

	commandID := idempotencyKey(message)
	if commandID == "" {
		// 没有 ID，无法做幂等性检查，直接执行
		return next(ctx, message)
	}

	if batch, ok := m.publishAllBatchFromContext(ctx); ok {
		return m.handleInPublishAllBatch(ctx, message, next, commandID, batch)
	}

	// 单条也不能持有外层聚合锁等待批量命令锁，否则可能形成循环等待。
	lock := m.acquireLock(commandID)
	if !lock.mu.TryLock() {
		m.releaseLock(commandID, lock)
		return errors.NewCode(errors.Concurrency, "command is already being processed").WithContext("command_id", commandID)
	}
	defer func() {
		lock.mu.Unlock()
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

func idempotencyKey(message messaging.IMessage) string {
	if message == nil {
		return ""
	}
	if md := message.GetMetadata(); md != nil {
		if key, ok := md.GetString(command.MetadataIdempotencyKey); ok {
			if key = strings.TrimSpace(key); key != "" {
				return key
			}
		}
	}
	return message.GetID()
}

func (m *IdempotencyMiddleware) Name() string {
	return "CommandIdempotency"
}
