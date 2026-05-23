package outbox

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/logging"
	"gochen/messaging"
)

// 并发安全的 Outbox 仓储 mock，用于 ParallelPublisher 并发测试。
type concurrentOutboxRepo struct {
	mu        sync.Mutex
	entries   map[int64]*OutboxEntry[int64]
	published map[int64]int // entryID -> 次数
	failed    map[int64]int // entryID -> 次数
}

func newConcurrentOutboxRepo(entries []OutboxEntry[int64]) *concurrentOutboxRepo {
	m := &concurrentOutboxRepo{
		entries:   make(map[int64]*OutboxEntry[int64], len(entries)),
		published: make(map[int64]int),
		failed:    make(map[int64]int),
	}
	for i := range entries {
		e := entries[i]
		ee := e
		m.entries[ee.ID] = &ee
	}
	return m
}

// SaveWithEvents ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - aggregateID：对象/实体标识
// - events：事件列表（待追加/发布）（类型：[]eventing.Event[int64]）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *concurrentOutboxRepo) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event[int64]) error {
	return nil
}

// GetPendingEntries 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：OutboxEntry[int64]）
// - err：错误信息（nil 表示成功）
func (r *concurrentOutboxRepo) ClaimPendingEntries(ctx context.Context, limit int) ([]OutboxEntry[int64], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]OutboxEntry[int64], 0, limit)
	for _, e := range r.entries {
		if e.Status == OutboxStatusPending {
			e.Status = OutboxStatusProcessing
			e.ClaimToken = fmt.Sprintf("claim-%d", e.ID)
			leaseUntil := time.Now().Add(time.Minute)
			e.LeaseUntil = &leaseUntil
			e.NextRetryAt = nil
			result = append(result, *e)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

// MarkAsPublished ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryID：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *concurrentOutboxRepo) MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[entryID]; ok && e.ClaimToken == claimToken {
		e.Status = OutboxStatusPublished
		e.ClaimToken = ""
		e.LeaseUntil = nil
	}
	r.published[entryID]++
	return nil
}

// MarkAsFailed ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryID：对象/实体标识
// - errorMsg：错误信息（类型：string）
// - nextRetryAt：参数值（具体语义见函数上下文）（类型：time.Time）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *concurrentOutboxRepo) MarkAsFailed(ctx context.Context, entryID int64, claimToken string, errorMsg string, nextRetryAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[entryID]; ok && e.ClaimToken == claimToken {
		e.Status = OutboxStatusFailed
		e.ClaimToken = ""
		e.LeaseUntil = nil
		e.LastError = errorMsg
		e.NextRetryAt = &nextRetryAt
		e.RetryCount++
	}
	r.failed[entryID]++
	return nil
}

func (r *concurrentOutboxRepo) RenewClaim(ctx context.Context, entryID int64, claimToken string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[entryID]; ok && e.ClaimToken == claimToken {
		leaseUntil := time.Now().Add(defaultClaimLease)
		e.LeaseUntil = &leaseUntil
		return nil
	}
	return nil
}

// DeletePublished 删除对象并同步到存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - olderThan：阈值（用于过滤更早的数据）（类型：time.Time）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *concurrentOutboxRepo) DeletePublished(ctx context.Context, olderThan time.Time) error {
	return nil
}

// stats 统计已发布/失败记录数量。
//
// 说明：
//
// 返回：
// - published：数值结果
// - failed：数值结果
func (r *concurrentOutboxRepo) stats() (published, failed int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.published {
		published += n
	}
	for _, n := range r.failed {
		failed += n
	}
	return
}

// 并发安全的事件总线 mock：仅计数 PublishEvent 次数。
type concurrentEventBus struct {
	count int32
}

// PublishEvent 发布事件到事件总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - evt：事件数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (b *concurrentEventBus) PublishEvent(ctx context.Context, evt eventing.IEvent) error {
	atomic.AddInt32(&b.count, 1)
	return nil
}

// PublishEvents 批量发布事件到事件总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - events：事件数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (b *concurrentEventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	for range events {
		atomic.AddInt32(&b.count, 1)
	}
	return nil
}

// 以下为 bus.IEventBus 其余方法的 no-op 实现

// Publish 发布消息到消息总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - message：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (b *concurrentEventBus) Publish(ctx context.Context, message messaging.IMessage) error {
	return nil
}

// PublishAll 发布消息到消息总线。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - messages：消息数据
//
// 返回：
// - err：错误信息（nil 表示成功）
func (b *concurrentEventBus) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	return nil
}

// Subscribe 订阅消息并注册处理器。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - msgType：参数值（具体语义见函数上下文）（类型：string）
// - handler：事件处理器
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (b *concurrentEventBus) Subscribe(ctx context.Context, msgType string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

// Use 追加中间件。
//
// 参数：
// - middleware：中间件列表（类型：messaging.IMiddleware）
func (b *concurrentEventBus) Use(middleware messaging.IMiddleware) {}

// Handlers result：列表结果（元素类型：messaging.IMessageHandler）。
//
// 返回：
func (b *concurrentEventBus) Handlers() []messaging.IMessageHandler { return nil }

// SubscribeEvent 订阅指定类型的事件并注册处理器。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - eventType：事件类型
// - handler：事件处理器
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (b *concurrentEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

// SubscribeHandler 按处理器声明的事件类型批量订阅。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - handler：事件处理器
//
// 返回：
// - result1：取消订阅函数（调用后解除订阅）
// - err：错误信息（nil 表示成功）
func (b *concurrentEventBus) SubscribeHandler(ctx context.Context, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

// 其余 bus 接口为本测试不关心的 no-op

// dlqRecordingRepo 用于验证 ParallelPublisher 在失败时的 DLQ 语义。
type dlqRecordingRepo struct {
	mu     sync.Mutex
	failed []struct {
		id        int64
		errorMsg  string
		nextRetry time.Time
	}
}

// SaveWithEvents ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - aggregateID：对象/实体标识
// - events：事件列表（待追加/发布）（类型：[]eventing.Event[int64]）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *dlqRecordingRepo) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event[int64]) error {
	return nil
}

// GetPendingEntries 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：OutboxEntry[int64]）
// - err：错误信息（nil 表示成功）
func (r *dlqRecordingRepo) ClaimPendingEntries(ctx context.Context, limit int) ([]OutboxEntry[int64], error) {
	return nil, nil
}

// MarkAsPublished ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryID：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *dlqRecordingRepo) MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error {
	return nil
}

// MarkAsFailed ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryID：对象/实体标识
// - errorMsg：错误信息（类型：string）
// - nextRetryAt：参数值（具体语义见函数上下文）（类型：time.Time）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *dlqRecordingRepo) MarkAsFailed(ctx context.Context, entryID int64, claimToken string, errorMsg string, nextRetryAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = append(r.failed, struct {
		id        int64
		errorMsg  string
		nextRetry time.Time
	}{
		id:        entryID,
		errorMsg:  errorMsg,
		nextRetry: nextRetryAt,
	})
	return nil
}

func (r *dlqRecordingRepo) RenewClaim(ctx context.Context, entryID int64, claimToken string) error {
	return nil
}

// DeletePublished 删除对象并同步到存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - olderThan：阈值（用于过滤更早的数据）（类型：time.Time）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *dlqRecordingRepo) DeletePublished(ctx context.Context, olderThan time.Time) error {
	return nil
}

// FailedCount result：数量/计数。
//
// 返回：
func (r *dlqRecordingRepo) FailedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.failed)
}

type dlqRecordingDLQ struct {
	mu    sync.Mutex
	moved []OutboxEntry[int64]
}

// MoveToDLQ ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entry：Outbox 条目
//
// 返回：
// - err：错误信息（nil 表示成功）
func (d *dlqRecordingDLQ) MoveToDLQ(ctx context.Context, entry OutboxEntry[int64]) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.moved = append(d.moved, entry)
	return nil
}

// GetDLQEntries 返回当前值。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：DLQEntry[int64]）
// - err：错误信息（nil 表示成功）
func (d *dlqRecordingDLQ) GetDLQEntries(ctx context.Context, limit int) ([]DLQEntry[int64], error) {
	return nil, nil
}

// RetryFromDLQ ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryID：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (d *dlqRecordingDLQ) RetryFromDLQ(ctx context.Context, entryID int64) error {
	return nil
}

// DeleteDLQEntry 删除数据。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - entryID：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (d *dlqRecordingDLQ) DeleteDLQEntry(ctx context.Context, entryID int64) error {
	return nil
}

// GetDLQCount 返回当前值。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (d *dlqRecordingDLQ) GetDLQCount(ctx context.Context) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return int64(len(d.moved)), nil
}

// Moved result：列表结果（元素类型：OutboxEntry[int64]）。
//
// 返回：
func (d *dlqRecordingDLQ) Moved() []OutboxEntry[int64] {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]OutboxEntry[int64], len(d.moved))
	copy(out, d.moved)
	return out
}

// TestParallelPublisher_ConcurrentWorkers_SinglePublishPerEntry 验证 ParallelPublisher ConcurrentWorkers SinglePublishPerEntry。
func TestParallelPublisher_ConcurrentWorkers_SinglePublishPerEntry(t *testing.T) {
	const (
		entryCount  = 500
		workerCount = 4
	)

	entries := make([]OutboxEntry[int64], 0, entryCount)
	for i := 0; i < entryCount; i++ {
		evt := newTestEvent(int64(i+1), 1, "TestEvent", nil)
		e, err := EventToOutboxEntry(evt.AggregateID, evt)
		require.NoError(t, err)
		e.ID = int64(i + 1)
		e.Status = OutboxStatusPending
		entries = append(entries, *e)
	}

	repo := newConcurrentOutboxRepo(entries)
	bus := &concurrentEventBus{}

	cfg := OutboxConfig{
		PublishInterval: 10 * time.Millisecond,
		BatchSize:       50,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
		CleanupInterval: 50 * time.Millisecond,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	p, err := NewParallelPublisher(repo, bus, cfg, logging.NewNoopLogger(), workerCount, reg, upgraders)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, p.Start(ctx))

	// 等待所有 entry 被处理（简单等待 + 检查）
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pub, _ := repo.stats()
		if pub == entryCount {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, p.Stop(stopCtx))

	pub, failed := repo.stats()
	if failed != 0 {
		t.Fatalf("expected no failed entries, got %d", failed)
	}
	if pub != entryCount {
		t.Fatalf("expected %d published entries, got %d", entryCount, pub)
	}

	if atomic.LoadInt32(&bus.count) != int32(entryCount) {
		t.Fatalf("expected %d events published to bus, got %d", entryCount, bus.count)
	}
}

// TestParallelPublisher_MarkFailed_MovesToDLQWhenMaxRetriesExceeded 验证 ParallelPublisher MarkFailed MovesToDLQWhenMaxRetriesExceeded。
func TestParallelPublisher_MarkFailed_MovesToDLQWhenMaxRetriesExceeded(t *testing.T) {
	repo := &dlqRecordingRepo{}
	bus := &concurrentEventBus{}

	cfg := OutboxConfig{
		RetryInterval: 30 * time.Second,
		MaxRetries:    3,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	p, err := NewParallelPublisher(repo, bus, cfg, logging.NewNoopLogger(), 1, reg, upgraders)
	require.NoError(t, err)
	dlq := &dlqRecordingDLQ{}
	p.SetDLQRepository(dlq)

	entry := OutboxEntry[int64]{
		ID:         42,
		RetryCount: cfg.MaxRetries - 1, // 再失败一次即达到上限
	}

	ctx := context.Background()
	p.markFailed(ctx, entry, "test-error")

	// 应该调用 MarkAsFailed 一次
	require.Equal(t, 1, repo.FailedCount())

	// 且应移入 DLQ
	moved := dlq.Moved()
	require.Len(t, moved, 1)
	assert.Equal(t, int64(42), moved[0].ID)
	assert.Equal(t, cfg.MaxRetries, moved[0].RetryCount)
	assert.Equal(t, "test-error", moved[0].LastError)
}

type aggregateOrderBus struct {
	mu sync.Mutex

	versions []uint64
	v1Seen   chan struct{}
	allowV1  chan struct{}
}

func newAggregateOrderBus() *aggregateOrderBus {
	return &aggregateOrderBus{
		v1Seen:  make(chan struct{}),
		allowV1: make(chan struct{}),
	}
}

// PublishEvent 在老实现（共享 workCh + 多 worker）下可稳定制造“同一聚合乱序”；
// 在新实现（按聚合分片）下会触发超时并保持顺序，从而避免测试 flake。
func (b *aggregateOrderBus) PublishEvent(ctx context.Context, evt eventing.IEvent) error {
	switch evt.GetVersion() {
	case 1:
		select {
		case <-b.v1Seen:
		default:
			close(b.v1Seen)
		}
		select {
		case <-b.allowV1:
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	case 2:
		select {
		case <-b.v1Seen:
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-b.allowV1:
		default:
			close(b.allowV1)
		}
	}

	b.mu.Lock()
	b.versions = append(b.versions, evt.GetVersion())
	b.mu.Unlock()
	return nil
}

func (b *aggregateOrderBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	for _, e := range events {
		if err := b.PublishEvent(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

func (b *aggregateOrderBus) Publish(ctx context.Context, message messaging.IMessage) error {
	return nil
}
func (b *aggregateOrderBus) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	return nil
}
func (b *aggregateOrderBus) Subscribe(ctx context.Context, msgType string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}
func (b *aggregateOrderBus) Use(middleware messaging.IMiddleware)  {}
func (b *aggregateOrderBus) Handlers() []messaging.IMessageHandler { return nil }
func (b *aggregateOrderBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}
func (b *aggregateOrderBus) SubscribeHandler(ctx context.Context, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	return func(ctx context.Context) error { return nil }, nil
}

func (b *aggregateOrderBus) Versions() []uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]uint64, len(b.versions))
	copy(out, b.versions)
	return out
}

func TestParallelPublisher_PreservesOrderWithinAggregate(t *testing.T) {
	evt1 := newTestEvent(1, 1, "e1", map[string]any{"n": 1})
	evt2 := newTestEvent(1, 2, "e2", map[string]any{"n": 2})

	e1, err := EventToOutboxEntry(evt1.AggregateID, evt1)
	require.NoError(t, err)
	e1.ID = 1
	e1.Status = OutboxStatusPending

	e2, err := EventToOutboxEntry(evt2.AggregateID, evt2)
	require.NoError(t, err)
	e2.ID = 2
	e2.Status = OutboxStatusPending

	repo := newOneShotOutboxRepo([]OutboxEntry[int64]{*e1, *e2})
	bus := newAggregateOrderBus()

	cfg := OutboxConfig{
		PublishInterval: 24 * time.Hour, // 测试中手动触发
		BatchSize:       2,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
		CleanupInterval: 24 * time.Hour,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	p, err := NewParallelPublisher(repo, bus, cfg, logging.NewNoopLogger(), 2, reg, upgraders)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, p.Start(ctx))
	require.NoError(t, p.PublishPending(ctx))
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, p.Stop(stopCtx))

	require.Equal(t, []uint64{1, 2}, bus.Versions())
}

func TestParallelPublisher_StopDrainsQueuedWork(t *testing.T) {
	const entryCount = 20

	entries := make([]OutboxEntry[int64], 0, entryCount)
	for i := 0; i < entryCount; i++ {
		evt := newTestEvent(1, uint64(i+1), fmt.Sprintf("e-%d", i+1), map[string]any{"i": i})
		e, err := EventToOutboxEntry(evt.AggregateID, evt)
		require.NoError(t, err)
		e.ID = int64(i + 1)
		e.Status = OutboxStatusPending
		entries = append(entries, *e)
	}

	repo := newOneShotOutboxRepo(entries)
	slowBus := &slowPublishBus{delay: 10 * time.Millisecond}

	cfg := OutboxConfig{
		PublishInterval: 24 * time.Hour, // 测试中手动触发
		BatchSize:       entryCount,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
		CleanupInterval: 24 * time.Hour,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	p, err := NewParallelPublisher(repo, slowBus, cfg, logging.NewNoopLogger(), 1, reg, upgraders)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, p.Start(ctx))
	require.NoError(t, p.PublishPending(ctx))
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, p.Stop(stopCtx))

	require.Equal(t, int32(entryCount), atomic.LoadInt32(&slowBus.count))
}

type slowPublishBus struct {
	concurrentEventBus
	delay time.Duration
}

func (b *slowPublishBus) PublishEvent(ctx context.Context, evt eventing.IEvent) error {
	time.Sleep(b.delay)
	atomic.AddInt32(&b.count, 1)
	return nil
}

func TestParallelPublisher_MarkPublishedRetriesWithFreshContextAfterSuccessfulPublish(t *testing.T) {
	entry := newProcessingOutboxEntry(t, 1)
	repo := &contextSensitiveMarkRepo{}
	bus := &concurrentEventBus{}
	p := &ParallelPublisher[int64]{
		repo:          repo,
		bus:           bus,
		cfg:           DefaultOutboxConfig(),
		log:           logging.NewNoopLogger(),
		eventRegistry: newTestRegistry(t),
		upgraders:     newTestUpgraders(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.processEntry(ctx, entry)

	require.Equal(t, int32(1), atomic.LoadInt32(&bus.count))
	require.GreaterOrEqual(t, atomic.LoadInt32(&repo.markPublishedCalls), int32(2))
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.markPublishedSuccesses))
}

func TestParallelPublisher_FetchOnceProcessesClaimedEntriesWhenContextCancelsDuringDispatch(t *testing.T) {
	entries := []OutboxEntry[int64]{
		newProcessingOutboxEntry(t, 1),
		newProcessingOutboxEntry(t, 2),
	}
	repo := &contextSensitiveMarkRepo{claimEntries: entries}
	bus := &concurrentEventBus{}
	p := &ParallelPublisher[int64]{
		repo:          repo,
		bus:           bus,
		cfg:           OutboxConfig{BatchSize: len(entries), ClaimLease: defaultClaimLease, ClaimRenewInterval: time.Second},
		log:           logging.NewNoopLogger(),
		workerCount:   1,
		workChs:       []chan OutboxEntry[int64]{make(chan OutboxEntry[int64])},
		stopCh:        make(chan struct{}),
		eventRegistry: newTestRegistry(t),
		upgraders:     newTestUpgraders(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := p.fetchOnce(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, int32(len(entries)), atomic.LoadInt32(&bus.count))
	require.Equal(t, int32(len(entries)), atomic.LoadInt32(&repo.markPublishedSuccesses))
}

func newProcessingOutboxEntry(t *testing.T, id int64) OutboxEntry[int64] {
	t.Helper()
	evt := newTestEvent(id, 1, fmt.Sprintf("event-%d", id), nil)
	entry, err := EventToOutboxEntry(evt.AggregateID, evt)
	require.NoError(t, err)
	entry.ID = id
	entry.Status = OutboxStatusProcessing
	entry.ClaimToken = fmt.Sprintf("claim-%d", id)
	leaseUntil := time.Now().Add(time.Minute)
	entry.LeaseUntil = &leaseUntil
	return *entry
}

type contextSensitiveMarkRepo struct {
	claimEntries []OutboxEntry[int64]

	markPublishedCalls     int32
	markPublishedSuccesses int32
}

func (r *contextSensitiveMarkRepo) SaveWithEvents(context.Context, int64, []eventing.Event[int64]) error {
	return nil
}

func (r *contextSensitiveMarkRepo) ClaimPendingEntries(context.Context, int) ([]OutboxEntry[int64], error) {
	entries := append([]OutboxEntry[int64](nil), r.claimEntries...)
	r.claimEntries = nil
	return entries, nil
}

func (r *contextSensitiveMarkRepo) MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error {
	_, _ = entryID, claimToken
	atomic.AddInt32(&r.markPublishedCalls, 1)
	if err := ctx.Err(); err != nil {
		return err
	}
	atomic.AddInt32(&r.markPublishedSuccesses, 1)
	return nil
}

func (r *contextSensitiveMarkRepo) MarkAsFailed(context.Context, int64, string, string, time.Time) error {
	return nil
}

func (r *contextSensitiveMarkRepo) RenewClaim(context.Context, int64, string) error {
	return nil
}

func (r *contextSensitiveMarkRepo) DeletePublished(context.Context, time.Time) error {
	return nil
}
