package outbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/errors"
	"gochen/observe/logging"
)

type blockingClaimRepository struct {
	IOutboxRepository[int64]
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingClaimRepository) ClaimPendingEntries(ctx context.Context, limit int) ([]OutboxEntry[int64], error) {
	r.once.Do(func() { close(r.entered) })
	select {
	case <-r.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestParallelPublisher_CtxDone_IsTerminal 验证 ctx.Done 会触发并行发布器进入 terminal（不可再次 Start）。
func TestParallelPublisher_CtxDone_IsTerminal(t *testing.T) {
	repo := newConcurrentOutboxRepo(nil)
	bus := &concurrentEventBus{}

	cfg := OutboxConfig{
		PublishInterval: 2 * time.Millisecond,
		BatchSize:       10,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
		CleanupInterval: 10 * time.Millisecond,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	p, err := NewParallelPublisher(repo, bus, cfg, logging.NewNoopLogger(), 1, reg, upgraders)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, p.Start(ctx))

	cancel()

	// 等待 ctx watcher 触发 Stop 并将 publisher 标记为 stopped（terminal）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err := p.Start(context.Background())
		if err != nil {
			require.Truef(t, errors.Is(err, errors.InvalidInput), "expected INVALID_INPUT after ctx.Done, got: %v", err)
			require.True(t, errors.Is(p.PublishPending(context.Background()), errors.FailedPrecondition))
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("expected ctx.Done to make publisher terminal (Start should eventually fail)")
}

// TestParallelPublisher_PublishPendingRefusesAfterWorkChannelsClosed 验证 worker 输入通道关闭后，
// PublishPending 复查 workClosed 并拒绝分发，避免与 Stop/ctx 取消竞争时向已关闭通道发送导致 panic。
func TestParallelPublisher_PublishPendingRefusesAfterWorkChannelsClosed(t *testing.T) {
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
		markCh:        make(chan markOp[int64], 1),
		stopCh:        make(chan struct{}),
		eventRegistry: newTestRegistry(t),
		upgraders:     newTestUpgraders(),
	}
	p.mu.Lock()
	p.started = true
	p.mu.Unlock()

	// 模拟 fetchLoop/Stop 已关闭 worker 通道并置位 workClosed。
	p.dispatchMu.Lock()
	p.closeWorkChannels()
	close(p.markCh)
	p.dispatchMu.Unlock()

	// 不应 panic（不会再向已关闭的 workCh/markCh 发送），且返回未启动错误。
	err := p.PublishPending(context.Background())
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.FailedPrecondition))
}

func TestParallelPublisher_StartStopWithBatchOperationsDoesNotLeakMarkLoop(t *testing.T) {
	repo := newConcurrentOutboxRepo(nil)
	bus := &concurrentEventBus{}
	cfg := OutboxConfig{
		PublishInterval: 2 * time.Millisecond,
		BatchSize:       10,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
		CleanupInterval: 10 * time.Millisecond,
	}
	p, err := NewParallelPublisher(repo, bus, cfg, logging.NewNoopLogger(), 1, newTestRegistry(t), newTestUpgraders())
	require.NoError(t, err)
	require.NoError(t, p.SetBatchOperations(&recordingBatchOps{}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = p.Start(ctx)
	}()
	go func() {
		defer wg.Done()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		_ = p.Stop(stopCtx)
	}()
	wg.Wait()

	p.mu.Lock()
	started := p.started
	p.mu.Unlock()
	if started {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		require.NoError(t, p.Stop(stopCtx))
		stopCancel()
	}

	p.mu.Lock()
	markDoneCh := p.markDoneCh
	p.mu.Unlock()
	require.NotNil(t, markDoneCh)

	select {
	case <-markDoneCh:
	case <-time.After(time.Second):
		t.Fatal("expected markLoop to stop after concurrent Start/Stop")
	}
}

func TestParallelPublisher_ConcurrentStopWaitsForManualPublish(t *testing.T) {
	repo := &blockingClaimRepository{
		IOutboxRepository: newConcurrentOutboxRepo(nil),
		entered:           make(chan struct{}),
		release:           make(chan struct{}),
	}
	cfg := OutboxConfig{PublishInterval: time.Hour, CleanupInterval: time.Hour, BatchSize: 1, RetryInterval: time.Second, RetentionPeriod: time.Hour}
	p, err := NewParallelPublisher(repo, &concurrentEventBus{}, cfg, logging.NewNoopLogger(), 1, newTestRegistry(t), newTestUpgraders())
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, p.Start(ctx))

	publishDone := make(chan error, 1)
	go func() { publishDone <- p.PublishPending(ctx) }()
	<-repo.entered

	stopDone := make(chan error, 2)
	go func() { stopDone <- p.Stop(ctx) }()
	go func() { stopDone <- p.Stop(ctx) }()
	for i := 0; i < 2; i++ {
		select {
		case err := <-stopDone:
			t.Fatalf("Stop returned before PublishPending completed: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}

	close(repo.release)
	require.NoError(t, <-publishDone)
	require.NoError(t, <-stopDone)
	require.NoError(t, <-stopDone)
}
