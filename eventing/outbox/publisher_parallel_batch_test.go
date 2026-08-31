package outbox

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/errors"
	"gochen/eventing"
	"gochen/observe/logging"
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
	markPublishedErr     error
	publishedErr         []error
	publishedHasDeadline []bool
	publishedCh          chan error
	publishedSnapshotCh  chan markBatchContextSnapshot
}

type cancelDuringPublishedBatchOps struct {
	started chan struct{}
	release chan struct{}
}

func (b *cancelDuringPublishedBatchOps) MarkAsPublishedBatch(ctx context.Context, entries []ClaimedEntry) error {
	close(b.started)
	<-b.release
	return errors.NewCode(errors.Database, "batch mark publish canceled")
}

func (b *cancelDuringPublishedBatchOps) MarkAsFailedBatch(context.Context, []FailedEntry) error {
	return nil
}

func (b *cancelDuringPublishedBatchOps) DeletePublishedBatch(context.Context, []int64) error {
	return nil
}

type cancelDuringFailedBatchOps struct {
	started chan struct{}
	release chan struct{}
}

func (b *cancelDuringFailedBatchOps) MarkAsPublishedBatch(context.Context, []ClaimedEntry) error {
	return nil
}

func (b *cancelDuringFailedBatchOps) MarkAsFailedBatch(ctx context.Context, entries []FailedEntry) error {
	close(b.started)
	<-b.release
	return errors.NewCode(errors.Database, "batch mark failed canceled")
}

func (b *cancelDuringFailedBatchOps) DeletePublishedBatch(context.Context, []int64) error {
	return nil
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
	return b.markPublishedErr
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
	go p.markLoop(ctx)

	p.markCh <- markOp[int64]{kind: markPublished, entryID: 1, claimToken: "claim-1"}
	cancel()
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

func TestParallelPublisher_MarkLoopFlushesAndStopsAfterContextCancel(t *testing.T) {
	batchOps := &recordingBatchOps{publishedCh: make(chan error, 1)}
	p := &ParallelPublisher[int64]{
		batchOps:   batchOps,
		cfg:        OutboxConfig{BatchSize: 10},
		markCh:     make(chan markOp[int64], 2),
		markDoneCh: make(chan struct{}),
		log:        logging.NewNoopLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	go p.markLoop(ctx)

	p.markCh <- markOp[int64]{kind: markPublished, entryID: 1, claimToken: "claim-1"}
	cancel()

	select {
	case err := <-batchOps.publishedCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for final mark flush")
	}

	p.markCh <- markOp[int64]{kind: markPublished, entryID: 2, claimToken: "claim-2"}
	close(p.markCh)

	select {
	case err := <-batchOps.publishedCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mark flush after canceled context")
	}
	select {
	case <-p.markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mark loop to stop")
	}
	require.Equal(t, []int64{1, 2}, batchOps.PublishedIDs())
}

type recordingAtomicDLQ struct {
	MockDLQRepository

	mu     sync.Mutex
	atomic []OutboxEntry[int64]
	err    error
}

func (d *recordingAtomicDLQ) MarkFailedAndMoveToDLQ(ctx context.Context, entry OutboxEntry[int64], claimToken string, errorMsg string) error {
	if d.err != nil {
		return d.err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	entry.ClaimToken = claimToken
	entry.LastError = errorMsg
	d.atomic = append(d.atomic, entry)
	return nil
}

func (d *recordingAtomicDLQ) AtomicMoved() []OutboxEntry[int64] {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]OutboxEntry[int64], len(d.atomic))
	copy(out, d.atomic)
	return out
}

var _ IAtomicDLQMover[int64] = (*recordingAtomicDLQ)(nil)

func TestParallelPublisher_MarkLoopBatchFailureUsesAtomicDLQMover(t *testing.T) {
	batchOps := &recordingBatchOps{}
	dlq := &recordingAtomicDLQ{}
	p := &ParallelPublisher[int64]{
		batchOps:   batchOps,
		dlq:        dlq,
		cfg:        OutboxConfig{BatchSize: 10},
		markCh:     make(chan markOp[int64], 2),
		markDoneCh: make(chan struct{}),
		log:        logging.NewNoopLogger(),
	}

	go p.markLoop(context.Background())

	dlqRetryAt := time.Now().Add(time.Minute)
	batchRetryAt := time.Now().Add(2 * time.Minute)
	p.markCh <- markOp[int64]{
		kind:       markFailed,
		entryID:    1,
		claimToken: "claim-dlq",
		errorMsg:   "dlq failure",
		nextRetry:  dlqRetryAt,
		moveToDLQ:  true,
		entry:      OutboxEntry[int64]{ID: 1, EventID: "event-dlq", RetryCount: 2, Status: OutboxStatusProcessing},
		dlqEntry:   OutboxEntry[int64]{ID: 1, EventID: "event-dlq", RetryCount: 3, Status: OutboxStatusFailed},
	}
	p.markCh <- markOp[int64]{
		kind:       markFailed,
		entryID:    2,
		claimToken: "claim-retry",
		errorMsg:   "retry failure",
		nextRetry:  batchRetryAt,
		moveToDLQ:  false,
	}
	close(p.markCh)

	select {
	case <-p.markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mark loop to stop")
	}

	atomicMoved := dlq.AtomicMoved()
	require.Len(t, atomicMoved, 1)
	require.Equal(t, int64(1), atomicMoved[0].ID)
	require.Equal(t, "claim-dlq", atomicMoved[0].ClaimToken)
	require.Equal(t, "dlq failure", atomicMoved[0].LastError)
	// 原子 mover 必须收到未自增的原始记录（RetryCount=2），由其内部自增到 3；
	// 若误传已自增的 dlqEntry（RetryCount=3）则会被再次自增成 4（M14 回归）。
	require.Equal(t, 2, atomicMoved[0].RetryCount)
	require.Empty(t, dlq.MovedEntries())

	require.Len(t, batchOps.failed, 1)
	require.Len(t, batchOps.failed[0], 1)
	require.Equal(t, int64(2), batchOps.failed[0][0].ID)
	require.Equal(t, "claim-retry", batchOps.failed[0][0].ClaimToken)
	require.Equal(t, "retry failure", batchOps.failed[0][0].Error)
}

func TestParallelPublisher_MarkLoopBatchPublishedFallbackRecoversPerEntryFailure(t *testing.T) {
	repo := &MockOutboxRepository{
		markPublishError: errors.NewCode(errors.Database, "mark publish failed"),
	}
	batchOps := &recordingBatchOps{
		markPublishedErr: errors.NewCode(errors.Database, "batch mark publish failed"),
	}
	p := &ParallelPublisher[int64]{
		repo:       repo,
		batchOps:   batchOps,
		cfg:        OutboxConfig{BatchSize: 10, RetryInterval: time.Minute, MaxRetries: 3},
		markCh:     make(chan markOp[int64], 1),
		markDoneCh: make(chan struct{}),
		log:        logging.NewNoopLogger(),
	}

	go p.markLoop(context.Background())
	p.markCh <- markOp[int64]{
		kind:       markPublished,
		entryID:    11,
		claimToken: "claim-11",
	}
	close(p.markCh)

	select {
	case <-p.markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mark loop to stop")
	}

	require.Equal(t, 1, repo.MarkedFailedLen())
	require.Equal(t, 0, repo.MarkedPublishedLen())
}

func TestParallelPublisher_MarkLoopPublishedFallbackSurvivesCancellationDuringBatch(t *testing.T) {
	repo := &contextSensitiveMarkRepo{}
	batchOps := &cancelDuringPublishedBatchOps{started: make(chan struct{}), release: make(chan struct{})}
	p := &ParallelPublisher[int64]{
		repo:       repo,
		batchOps:   batchOps,
		cfg:        OutboxConfig{BatchSize: 1, MarkPublishedAttempts: 1},
		markCh:     make(chan markOp[int64], 1),
		markDoneCh: make(chan struct{}),
		log:        logging.NewNoopLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	go p.markLoop(ctx)
	p.markCh <- markOp[int64]{kind: markPublished, entryID: 31, claimToken: "claim-31"}
	<-batchOps.started
	cancel()
	close(batchOps.release)
	close(p.markCh)
	select {
	case <-p.markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mark loop")
	}
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.markPublishedSuccesses))
}

func TestParallelPublisher_MarkLoopFailedFallbackSurvivesCancellationDuringBatch(t *testing.T) {
	repo := &recoverMarkFailureRepo{}
	batchOps := &cancelDuringFailedBatchOps{started: make(chan struct{}), release: make(chan struct{})}
	p := &ParallelPublisher[int64]{
		repo:       repo,
		batchOps:   batchOps,
		cfg:        OutboxConfig{BatchSize: 1},
		markCh:     make(chan markOp[int64], 1),
		markDoneCh: make(chan struct{}),
		log:        logging.NewNoopLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	go p.markLoop(ctx)
	p.markCh <- markOp[int64]{
		kind:       markFailed,
		entryID:    32,
		claimToken: "claim-32",
		errorMsg:   "failed",
		nextRetry:  time.Now().Add(time.Minute),
		entry:      OutboxEntry[int64]{ID: 32},
	}
	<-batchOps.started
	cancel()
	close(batchOps.release)
	close(p.markCh)
	select {
	case <-p.markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mark loop")
	}
	require.NoError(t, repo.ctxErr)
	require.Equal(t, int64(32), repo.failedEntryID)
}

func TestParallelPublisher_MarkLoopBatchPublishedFallbackIgnoresCanceledStopContext(t *testing.T) {
	repo := newOneShotOutboxRepo(nil)
	batchOps := &recordingBatchOps{
		markPublishedErr: errors.NewCode(errors.Database, "batch mark publish failed"),
	}
	stopCtx, stopCancel := context.WithCancel(context.Background())
	stopCancel()
	p := &ParallelPublisher[int64]{
		repo:        repo,
		batchOps:    batchOps,
		cfg:         OutboxConfig{BatchSize: 10, RetryInterval: time.Minute, MaxRetries: 3},
		markCh:      make(chan markOp[int64], 2),
		markDoneCh:  make(chan struct{}),
		markStopCtx: stopCtx,
		log:         logging.NewNoopLogger(),
	}

	go p.markLoop(context.Background())
	p.markCh <- markOp[int64]{kind: markPublished, entryID: 21, claimToken: "claim-21"}
	p.markCh <- markOp[int64]{kind: markPublished, entryID: 22, claimToken: "claim-22"}
	close(p.markCh)

	select {
	case <-p.markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mark loop to stop")
	}

	pubs, _ := repo.Stats()
	require.Equal(t, 2, pubs)
}

func TestParallelPublisher_MarkLoopBatchFailureFallsBackWhenAtomicDLQFails(t *testing.T) {
	batchOps := &recordingBatchOps{}
	dlq := &recordingAtomicDLQ{err: errors.NewCode(errors.Database, "atomic dlq failed")}
	repo := &MockOutboxRepository{}
	p := &ParallelPublisher[int64]{
		repo:       repo,
		batchOps:   batchOps,
		dlq:        dlq,
		cfg:        OutboxConfig{BatchSize: 10, RetryInterval: time.Minute, MaxRetries: 3},
		markCh:     make(chan markOp[int64], 1),
		markDoneCh: make(chan struct{}),
		log:        logging.NewNoopLogger(),
	}

	go p.markLoop(context.Background())
	p.markCh <- markOp[int64]{
		kind:       markFailed,
		entryID:    12,
		claimToken: "claim-12",
		errorMsg:   "publish failed",
		nextRetry:  time.Now().Add(time.Minute),
		moveToDLQ:  true,
		dlqEntry:   OutboxEntry[int64]{ID: 12, EventID: "event-12", Status: OutboxStatusFailed},
	}
	close(p.markCh)

	select {
	case <-p.markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mark loop to stop")
	}

	require.Empty(t, dlq.AtomicMoved())
	require.Empty(t, batchOps.failed)
	require.Equal(t, 1, repo.MarkedFailedLen())

	moved := dlq.MovedEntries()
	require.Len(t, moved, 1)
	require.Equal(t, int64(12), moved[0].ID)
	require.Equal(t, "event-12", moved[0].EventID)
}

type blockingMarkPublishedRepo struct {
	MockOutboxRepository
	calls atomic.Int64
}

func (r *blockingMarkPublishedRepo) MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error {
	r.calls.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

func (r *blockingMarkPublishedRepo) MarkPublishCalls() int {
	return int(r.calls.Load())
}

func TestParallelPublisher_MarkLoopBatchFailureFallsBackToBatchWhenAtomicAndFallbackMarkFail(t *testing.T) {
	batchOps := &recordingBatchOps{}
	dlq := &recordingAtomicDLQ{err: errors.NewCode(errors.Database, "atomic dlq failed")}
	repo := &MockOutboxRepository{
		markFailedError: errors.NewCode(errors.Database, "fallback mark failed"),
	}
	p := &ParallelPublisher[int64]{
		repo:       repo,
		batchOps:   batchOps,
		dlq:        dlq,
		cfg:        OutboxConfig{BatchSize: 10, RetryInterval: time.Minute, MaxRetries: 3},
		markCh:     make(chan markOp[int64], 1),
		markDoneCh: make(chan struct{}),
		log:        logging.NewNoopLogger(),
	}

	go p.markLoop(context.Background())
	p.markCh <- markOp[int64]{
		kind:       markFailed,
		entryID:    13,
		claimToken: "claim-13",
		errorMsg:   "publish failed",
		nextRetry:  time.Now().Add(time.Minute),
		moveToDLQ:  true,
		dlqEntry:   OutboxEntry[int64]{ID: 13, EventID: "event-13", Status: OutboxStatusFailed},
	}
	close(p.markCh)

	select {
	case <-p.markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mark loop to stop")
	}

	require.Empty(t, dlq.AtomicMoved())
	moved := dlq.MovedEntries()
	require.Len(t, moved, 1)
	require.Equal(t, int64(13), moved[0].ID)
	require.Len(t, batchOps.failed, 1)
	require.Len(t, batchOps.failed[0], 1)
	require.Equal(t, int64(13), batchOps.failed[0][0].ID)
	require.Equal(t, 0, repo.MarkedFailedLen())
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
	require.NoError(t, p.SetBatchOperations(batchOps))

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
