package outbox

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/logging"
)

type oneShotOutboxRepo struct {
	mu sync.Mutex

	entries []OutboxEntry[int64]
	pubs    int
	fails   int
}

func newOneShotOutboxRepo(entries []OutboxEntry[int64]) *oneShotOutboxRepo {
	cp := append([]OutboxEntry[int64](nil), entries...)
	return &oneShotOutboxRepo{entries: cp}
}

// SaveWithEvents ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - aggregateID：对象/实体标识
// - events：事件列表（待追加/发布）（类型：[]eventing.Event[int64]）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *oneShotOutboxRepo) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event[int64]) error {
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
func (r *oneShotOutboxRepo) ClaimPendingEntries(ctx context.Context, limit int) ([]OutboxEntry[int64], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > len(r.entries) {
		limit = len(r.entries)
	}
	out := append([]OutboxEntry[int64](nil), r.entries[:limit]...)
	for i := range out {
		out[i].Status = OutboxStatusProcessing
		out[i].ClaimToken = "one-shot-claim"
		leaseUntil := time.Now().Add(time.Minute)
		out[i].LeaseUntil = &leaseUntil
		out[i].NextRetryAt = nil
	}
	// one-shot：取出即移除，避免依赖 MarkAsPublished 更新状态
	r.entries = r.entries[limit:]
	return out, nil
}

// MarkAsPublished ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryID：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *oneShotOutboxRepo) MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pubs++
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
func (r *oneShotOutboxRepo) MarkAsFailed(ctx context.Context, entryID int64, claimToken string, errorMsg string, nextRetryAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fails++
	return nil
}

func (r *oneShotOutboxRepo) RenewClaim(ctx context.Context, entryID int64, claimToken string) error {
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
func (r *oneShotOutboxRepo) DeletePublished(ctx context.Context, olderThan time.Time) error {
	return nil
}

// Stats pubs：数值结果。
//
// 返回：
// - fails：数值结果
func (r *oneShotOutboxRepo) Stats() (pubs, fails int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pubs, r.fails
}

type recordingBatchOps struct {
	mu sync.Mutex

	published            [][]ClaimedEntry
	failed               [][]FailedEntry
	publishedErr         []error
	publishedHasDeadline []bool
	publishedCh          chan error
	publishedSnapshotCh  chan markBatchContextSnapshot
}

type markBatchContextSnapshot struct {
	err         error
	hasDeadline bool
}

// MarkAsPublishedBatch ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryIDs：对象/实体标识列表
//
// 返回：
// - err：错误信息（nil 表示成功）
func (b *recordingBatchOps) MarkAsPublishedBatch(ctx context.Context, entryIDs []ClaimedEntry) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := append([]ClaimedEntry(nil), entryIDs...)
	b.published = append(b.published, cp)
	ctxErr := ctx.Err()
	_, hasDeadline := ctx.Deadline()
	b.publishedErr = append(b.publishedErr, ctxErr)
	b.publishedHasDeadline = append(b.publishedHasDeadline, hasDeadline)
	if b.publishedCh != nil {
		select {
		case b.publishedCh <- ctxErr:
		default:
		}
	}
	if b.publishedSnapshotCh != nil {
		select {
		case b.publishedSnapshotCh <- markBatchContextSnapshot{err: ctxErr, hasDeadline: hasDeadline}:
		default:
		}
	}
	return nil
}

// MarkAsFailedBatch ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entries：参数值（具体语义见函数上下文）（类型：[]FailedEntry）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (b *recordingBatchOps) MarkAsFailedBatch(ctx context.Context, entries []FailedEntry) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := append([]FailedEntry(nil), entries...)
	b.failed = append(b.failed, cp)
	return nil
}

// DeletePublishedBatch 删除对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - entryIDs：对象/实体标识列表
//
// 返回：
// - err：错误信息（nil 表示成功）
func (b *recordingBatchOps) DeletePublishedBatch(ctx context.Context, entryIDs []int64) error {
	return nil
}

// PublishedIDs 发布消息到消息总线。
//
// 返回：
// - result：列表结果（元素类型：int64）
func (b *recordingBatchOps) PublishedIDs() []int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []int64
	for _, batch := range b.published {
		for _, entry := range batch {
			out = append(out, entry.ID)
		}
	}
	return out
}

func TestParallelPublisher_MarkLoopFinalFlushUsesFreshContext(t *testing.T) {
	repo := newOneShotOutboxRepo(nil)
	batchOps := &recordingBatchOps{publishedCh: make(chan error, 1)}
	p := &ParallelPublisher[int64]{
		repo:       repo,
		batchOps:   batchOps,
		cfg:        OutboxConfig{BatchSize: 10},
		markCh:     make(chan markOp[int64], 1),
		markDoneCh: make(chan struct{}),
		log:        logging.NewNoopLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	go p.markLoop(ctx)

	p.markCh <- markOp[int64]{kind: markPublished, entryID: 1, claimToken: "claim-1"}
	close(p.markCh)

	select {
	case err := <-batchOps.publishedCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for final mark flush")
	}
	select {
	case <-p.markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mark loop to stop")
	}
	require.Equal(t, []int64{1}, batchOps.PublishedIDs())
}

func TestParallelPublisher_MarkLoopFinalFlushUsesBoundedContextOnNormalStop(t *testing.T) {
	repo := newOneShotOutboxRepo(nil)
	batchOps := &recordingBatchOps{publishedSnapshotCh: make(chan markBatchContextSnapshot, 1)}
	p := &ParallelPublisher[int64]{
		repo:       repo,
		batchOps:   batchOps,
		cfg:        OutboxConfig{BatchSize: 10},
		markCh:     make(chan markOp[int64], 1),
		markDoneCh: make(chan struct{}),
		log:        logging.NewNoopLogger(),
	}

	go p.markLoop(context.Background())

	p.markCh <- markOp[int64]{kind: markPublished, entryID: 1, claimToken: "claim-1"}
	close(p.markCh)

	select {
	case snapshot := <-batchOps.publishedSnapshotCh:
		require.NoError(t, snapshot.err)
		require.True(t, snapshot.hasDeadline)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for final mark flush")
	}
	select {
	case <-p.markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mark loop to stop")
	}
	require.Equal(t, []int64{1}, batchOps.PublishedIDs())
}

// TestParallelPublisher_BatchMarking_PrefersBatchOps 验证 ParallelPublisher BatchMarking PrefersBatchOps。
func TestParallelPublisher_BatchMarking_PrefersBatchOps(t *testing.T) {
	const (
		entryCount  = 50
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

	repo := newOneShotOutboxRepo(entries)
	bus := &concurrentEventBus{}
	batchOps := &recordingBatchOps{}

	cfg := OutboxConfig{
		PublishInterval: 24 * time.Hour, // 避免自动 ticker 干扰，测试中手动触发
		BatchSize:       entryCount,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
		CleanupInterval: 24 * time.Hour,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	p, err := NewParallelPublisher(repo, bus, cfg, logging.NewNoopLogger(), workerCount, reg, upgraders)
	require.NoError(t, err)
	p.SetBatchOperations(batchOps)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, p.Start(ctx))
	require.NoError(t, p.PublishPending(ctx))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&bus.count) == int32(entryCount) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, p.Stop(stopCtx))

	pubs, fails := repo.Stats()
	require.Equal(t, 0, fails)
	// 批量标记成功时，逐条 MarkAsPublished 不应被调用
	require.Equal(t, 0, pubs)

	publishedIDs := batchOps.PublishedIDs()
	require.Len(t, publishedIDs, entryCount)
}
