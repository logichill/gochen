package middleware

import (
	"context"
	"sort"
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

	// stopCleanup 停止清理的信号
	stopCleanup chan struct{}

	// stopOnce 确保 Stop(ctx) 只执行一次，避免重复关闭 channel
	stopOnce sync.Once

	// clock 用于 TTL/cleanup 的时间来源（便于测试稳定控制）。
	clock clock.IClock
}

type idLock struct {
	mu   sync.Mutex
	refs int
}

type publishAllBatchContextKey struct {
	middleware *IdempotencyMiddleware
}

type publishAllBatch struct {
	mu        sync.Mutex
	pending   map[string]struct{}
	callbacks []func(success bool)
	finished  bool
	success   bool
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
	if config.Clock == nil {
		config.Clock = clock.NewRealClock()
	}

	m := &IdempotencyMiddleware{
		processed:       make(map[string]time.Time),
		locks:           make(map[string]*idLock),
		ttl:             config.TTL,
		cleanupInterval: config.CleanupInterval,
		maxProcessed:    config.MaxProcessed,
		stopCleanup:     make(chan struct{}),
		clock:           config.Clock,
	}

	// 启动后台清理 goroutine
	go m.startCleanupWorker()

	return m
}

// BeginPublish 为即时发布隔离继承的批次状态，避免已投递命令被外层批次回滚。
// 保留 ctx 的其他值、取消与截止时间；ctx 不含当前实例的批次时原样返回。
func (m *IdempotencyMiddleware) BeginPublish(ctx context.Context) context.Context {
	if _, ok := m.publishAllBatchFromContext(ctx); !ok {
		return ctx
	}
	return context.WithValue(ctx, publishAllBatchContextKey{middleware: m}, (*publishAllBatch)(nil))
}

// BeginPublishAllBatch 创建批量发布提交作用域。
//
// MessageBus.PublishAll 在多消息批量路径中会先逐条执行中间件，再执行一次真实
// transport.PublishAll。命令幂等成功标记必须等 transport 批量提交成功后才能记录。
// 批量持有命令锁到投递完成；锁已被占用时返回 Concurrency，调用方可重试整个批次。
func (m *IdempotencyMiddleware) BeginPublishAllBatch(ctx context.Context) (context.Context, func(success bool)) {
	if ctx == nil {
		ctx = context.Background()
	}
	batch := &publishAllBatch{
		pending: make(map[string]struct{}),
	}
	return context.WithValue(ctx, publishAllBatchContextKey{middleware: m}, batch), batch.finish
}

// BeginPublishAllAttempt 为 MessageBus 的同步中间件调用创建幂等状态检查点。
// 结束函数可重复调用；失败时回滚该检查点后的预留和提交回调，成功时保留到整批完成。
// ctx 不属于当前中间件的批次时返回 nil。
func (m *IdempotencyMiddleware) BeginPublishAllAttempt(ctx context.Context) func(success bool) {
	batch, ok := m.publishAllBatchFromContext(ctx)
	if !ok {
		return nil
	}
	batch.mu.Lock()
	checkpoint := len(batch.callbacks)
	batch.mu.Unlock()
	finished := false
	return func(success bool) {
		batch.mu.Lock()
		if finished || batch.finished {
			batch.mu.Unlock()
			return
		}
		finished = true
		if success || checkpoint >= len(batch.callbacks) {
			batch.mu.Unlock()
			return
		}
		callbacks := append([]func(bool){}, batch.callbacks[checkpoint:]...)
		clear(batch.callbacks[checkpoint:])
		batch.callbacks = batch.callbacks[:checkpoint]
		batch.mu.Unlock()
		for i := len(callbacks) - 1; i >= 0; i-- {
			callbacks[i](false)
		}
	}
}

// Handle 仅对命令消息做幂等保护，并保证同一命令 ID 不会被并发重复执行。
// 同一幂等键正在处理时返回 Concurrency，调用方可重试当前操作。
func (m *IdempotencyMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
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

func (m *IdempotencyMiddleware) handleInPublishAllBatch(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc, commandID string, batch *publishAllBatch) error {
	if !batch.reserve(commandID) {
		return nil
	}
	// 锁争用、next 失败或 panic 后撤销预留，允许同一批次作用域重新尝试。
	keepReservation := false
	defer func() {
		if !keepReservation {
			batch.unreserve(commandID)
		}
	}()

	lock := m.acquireLock(commandID)
	// 批次可能以相反顺序获取多个命令锁，不能持有前面的锁再阻塞等待后面的锁。
	if !lock.mu.TryLock() {
		m.releaseLock(commandID, lock)
		return errors.NewCode(errors.Concurrency, "command is already being processed").WithContext("command_id", commandID)
	}

	releaseOnReturn := true
	defer func() {
		if releaseOnReturn {
			lock.mu.Unlock()
			m.releaseLock(commandID, lock)
		}
	}()

	if m.isProcessed(commandID) {
		batch.addCallback(func(success bool) {
			if !success {
				batch.unreserve(commandID)
			}
		})
		keepReservation = true
		return nil
	}

	err := next(ctx, message)
	if err != nil {
		return err
	}

	releaseOnReturn = false
	batch.addCallback(func(success bool) {
		defer func() {
			lock.mu.Unlock()
			m.releaseLock(commandID, lock)
		}()
		if success {
			m.markProcessed(commandID)
		} else {
			batch.unreserve(commandID)
		}
	})
	keepReservation = true
	return nil
}

func (m *IdempotencyMiddleware) publishAllBatchFromContext(ctx context.Context) (*publishAllBatch, bool) {
	if ctx == nil {
		return nil, false
	}
	batch, ok := ctx.Value(publishAllBatchContextKey{middleware: m}).(*publishAllBatch)
	return batch, ok && batch != nil
}

func (b *publishAllBatch) reserve(commandID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.pending[commandID]; ok {
		return false
	}
	b.pending[commandID] = struct{}{}
	return true
}

func (b *publishAllBatch) unreserve(commandID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, commandID)
}

func (b *publishAllBatch) addCallback(callback func(success bool)) {
	b.mu.Lock()
	if b.finished {
		success := b.success
		b.mu.Unlock()
		callback(success)
		return
	}
	b.callbacks = append(b.callbacks, callback)
	b.mu.Unlock()
}

func (b *publishAllBatch) finish(success bool) {
	b.mu.Lock()
	if b.finished {
		b.mu.Unlock()
		return
	}
	b.finished = true
	b.success = success
	callbacks := append([]func(bool){}, b.callbacks...)
	b.callbacks = nil
	b.mu.Unlock()

	for i := len(callbacks) - 1; i >= 0; i-- {
		callbacks[i](success)
	}
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

// isProcessed 判断命令 ID 是否仍在幂等窗口内被视为“已处理”。
func (m *IdempotencyMiddleware) isProcessed(commandID string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	processedAt, exists := m.processed[commandID]
	if !exists {
		return false
	}

	// 检查是否过期
	if m.clock.Now().Sub(processedAt) > m.ttl {
		return false
	}

	return true
}

// markProcessed 把命令 ID 记录为已处理，并在必要时触发最老记录淘汰。
func (m *IdempotencyMiddleware) markProcessed(commandID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	now := m.clock.Now()
	_, existed := m.processed[commandID]
	m.processed[commandID] = now
	if existed {
		return
	}

	if m.maxProcessed > 0 && len(m.processed) > m.maxProcessed {
		overflow := len(m.processed) - m.maxProcessed
		m.evictOldest(overflow)
	}
}

// evictOldest 按处理时间从旧到新淘汰最老的一批命令 ID 记录。
func (m *IdempotencyMiddleware) evictOldest(n int) {
	if n <= 0 || len(m.processed) == 0 {
		return
	}
	if n >= len(m.processed) {
		m.processed = make(map[string]time.Time)
		return
	}

	type kv struct {
		id string
		t  time.Time
	}
	items := make([]kv, 0, len(m.processed))
	for id, t := range m.processed {
		items = append(items, kv{id: id, t: t})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].t.Before(items[j].t) })
	for i := 0; i < n; i++ {
		delete(m.processed, items[i].id)
	}
}

// acquireLock 返回命令 ID 对应的细粒度锁，并增加其引用计数。
func (m *IdempotencyMiddleware) acquireLock(commandID string) *idLock {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if lock, ok := m.locks[commandID]; ok {
		lock.refs++
		return lock
	}

	lock := &idLock{refs: 1}
	m.locks[commandID] = lock
	return lock
}

// releaseLock 释放一次命令锁引用，并在无人使用时回收锁条目。
func (m *IdempotencyMiddleware) releaseLock(commandID string, lock *idLock) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	current, ok := m.locks[commandID]
	if !ok || current != lock {
		return
	}

	lock.refs--
	if lock.refs <= 0 {
		delete(m.locks, commandID)
	}
}

// startCleanupWorker 周期性清理超过 TTL 的命令 ID 记录。
func (m *IdempotencyMiddleware) startCleanupWorker() {
	ticker, err := m.clock.NewTicker(m.cleanupInterval)
	if err != nil {
		return
	}
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C():
			m.cleanup()
		case <-m.stopCleanup:
			return
		}
	}
}

// cleanup 删除所有已经超过 TTL 的命令 ID 记录。
func (m *IdempotencyMiddleware) cleanup() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	now := m.clock.Now()
	for id, processedAt := range m.processed {
		if now.Sub(processedAt) > m.ttl {
			delete(m.processed, id)
		}
	}
}

// Stop 停止后台清理 worker，并保证重复调用也不会 panic。
func (m *IdempotencyMiddleware) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	m.stopOnce.Do(func() {
		close(m.stopCleanup)
	})
	return nil
}

// Clear 清空所有幂等记录，并保留正在使用的细粒度锁以维持并发串行化。
func (m *IdempotencyMiddleware) Clear() {
	if m == nil {
		return
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.processed = make(map[string]time.Time)
	for commandID, lock := range m.locks {
		if lock.refs <= 0 {
			delete(m.locks, commandID)
		}
	}
}

func (m *IdempotencyMiddleware) GetProcessedCount() int {
	if m == nil {
		return 0
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.processed)
}
