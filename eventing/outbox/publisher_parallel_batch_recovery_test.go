package outbox

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"gochen/errors"
	"gochen/observe/logging"
	"gochen/testkit/require"
)

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

	// 发布成功但标记发布失败时，mark 失败必须计入 RetryCount（有界重投 + DLQ 逃生）。
	// 回退路径传入的 recovery entry 无 RetryCount，MaxRetries=3 下不触发 DLQ。
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
