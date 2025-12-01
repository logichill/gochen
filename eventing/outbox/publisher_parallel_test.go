package outbox

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/logging"
	"gochen/messaging"
)

// 并发安全的 Outbox 仓储 mock，用于 ParallelPublisher 并发测试。
type concurrentOutboxRepo struct {
	mu        sync.Mutex
	entries   map[int64]*OutboxEntry
	published map[int64]int // entryID -> 次数
	failed    map[int64]int // entryID -> 次数
}

func newConcurrentOutboxRepo(entries []OutboxEntry) *concurrentOutboxRepo {
	m := &concurrentOutboxRepo{
		entries:   make(map[int64]*OutboxEntry, len(entries)),
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

func (r *concurrentOutboxRepo) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event) error {
	return nil
}

func (r *concurrentOutboxRepo) GetPendingEntries(ctx context.Context, limit int) ([]OutboxEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]OutboxEntry, 0, limit)
	for _, e := range r.entries {
		if e.Status == OutboxStatusPending {
			result = append(result, *e)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (r *concurrentOutboxRepo) MarkAsPublished(ctx context.Context, entryID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[entryID]; ok {
		e.Status = OutboxStatusPublished
	}
	r.published[entryID]++
	return nil
}

func (r *concurrentOutboxRepo) MarkAsFailed(ctx context.Context, entryID int64, errorMsg string, nextRetryAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[entryID]; ok {
		e.Status = OutboxStatusFailed
		e.LastError = errorMsg
		e.NextRetryAt = &nextRetryAt
		e.RetryCount++
	}
	r.failed[entryID]++
	return nil
}

func (r *concurrentOutboxRepo) DeletePublished(ctx context.Context, olderThan time.Time) error {
	return nil
}

// 统计已发布/失败记录数量
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

func (b *concurrentEventBus) PublishEvent(ctx context.Context, evt eventing.IEvent) error {
	atomic.AddInt32(&b.count, 1)
	return nil
}

func (b *concurrentEventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	for range events {
		atomic.AddInt32(&b.count, 1)
	}
	return nil
}

// 以下为 bus.IEventBus 其余方法的 no-op 实现

func (b *concurrentEventBus) Publish(ctx context.Context, message messaging.IMessage) error {
	return nil
}

func (b *concurrentEventBus) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	return nil
}

func (b *concurrentEventBus) Subscribe(ctx context.Context, msgType string, handler messaging.IMessageHandler) error {
	return nil
}

func (b *concurrentEventBus) Unsubscribe(ctx context.Context, msgType string, handler messaging.IMessageHandler) error {
	return nil
}

func (b *concurrentEventBus) Use(middleware messaging.IMiddleware) {}

func (b *concurrentEventBus) Handlers() []messaging.IMessageHandler { return nil }

func (b *concurrentEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) error {
	return nil
}

func (b *concurrentEventBus) UnsubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) error {
	return nil
}

func (b *concurrentEventBus) SubscribeHandler(ctx context.Context, handler bus.IEventHandler) error {
	return nil
}

func (b *concurrentEventBus) UnsubscribeHandler(ctx context.Context, handler bus.IEventHandler) error {
	return nil
}

// 其余 bus 接口为本测试不关心的 no-op

// TestParallelPublisher_ConcurrentWorkers_SinglePublishPerEntry
//
// 多 worker 场景下，验证同一批 pending 记录在并行处理时，每条记录最多被标记已发布一次，
// 且事件总线收到的事件条数与记录数一致（“无重复发布”）。
func TestParallelPublisher_ConcurrentWorkers_SinglePublishPerEntry(t *testing.T) {
	const (
		entryCount  = 500
		workerCount = 4
	)

	entries := make([]OutboxEntry, 0, entryCount)
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

	p := NewParallelPublisher(repo, bus, cfg, logging.NewNoopLogger(), workerCount)

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

	require.NoError(t, p.Stop())

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
