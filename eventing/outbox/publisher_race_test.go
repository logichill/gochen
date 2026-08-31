package outbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/errors"
	"gochen/eventing"
	"gochen/observe/logging"
)

// TestPublisher_ConcurrentStartStopAndPublishPending_NoRace 验证 Publisher 在并发 Start/Stop/PublishPending 下无竞态与 panic。
func TestPublisher_ConcurrentStartStopAndPublishPending_NoRace(t *testing.T) {
	const entryCount = 200

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
		PublishInterval: 2 * time.Millisecond,
		BatchSize:       25,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
		CleanupInterval: 10 * time.Millisecond,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	p, err := NewPublisher(repo, bus, cfg, logging.NewNoopLogger(), reg, upgraders)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 并发 Start（幂等）
	{
		var wg sync.WaitGroup
		wg.Add(8)
		for i := 0; i < 8; i++ {
			go func() {
				defer wg.Done()
				_ = p.Start(ctx)
			}()
		}
		wg.Wait()
	}

	// 并发 PublishPending（与后台 loop 并行）；不追求强断言，只锁竞态。
	{
		var wg sync.WaitGroup
		wg.Add(8)
		for i := 0; i < 8; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 30; j++ {
					_ = p.PublishPending(ctx)
				}
			}()
		}
		wg.Wait()
	}

	// 并发 Stop（幂等）
	{
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()

		var wg sync.WaitGroup
		wg.Add(8)
		for i := 0; i < 8; i++ {
			go func() {
				defer wg.Done()
				_ = p.Stop(stopCtx)
			}()
		}
		wg.Wait()
	}

	// Stop 后不可再次 Start
	require.Error(t, p.Start(ctx))

	// Stop 后 PublishPending 应返回 INVALID_INPUT。
	require.True(t, errors.Is(p.PublishPending(ctx), errors.InvalidInput))

	published, _ := repo.stats()
	require.Greater(t, published, 0, "expected some entries published")
}

func TestPublisher_ConcurrentRuntimeConfiguration_NoRace(t *testing.T) {
	p, err := NewPublisher(
		&MockOutboxRepository{},
		&MockEventBus{},
		DefaultOutboxConfig(),
		logging.NewNoopLogger(),
		newTestRegistry(t),
		newTestUpgraders(),
	)
	require.NoError(t, err)

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	metrics := &recordingPublisherMetrics{}
	dlq := &MockDLQRepository{}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = p.SetEventRegistry(reg)
				_ = p.SetUpgraderRegistry(upgraders)
				_ = p.SetMetricsRecorder(metrics)
				_ = p.SetDLQRepository(dlq)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = p.core()
			}
		}()
	}
	wg.Wait()
}

func TestPublisher_RuntimeConfigurationWaitsForPublishPending(t *testing.T) {
	evt := newTestEvent(int64(1), 1, "TestEvent", nil)
	entry, err := EventToOutboxEntry(evt.AggregateID, evt)
	require.NoError(t, err)
	entry.ID = 1
	entry.Status = OutboxStatusPending

	publishEntered := make(chan struct{})
	releasePublish := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePublish) }) }
	defer release()

	eventBus := &MockEventBus{publishEventFunc: func(context.Context, eventing.IEvent) error {
		close(publishEntered)
		<-releasePublish
		return nil
	}}
	p, err := NewPublisher(
		&MockOutboxRepository{entries: []OutboxEntry[int64]{*entry}},
		eventBus,
		DefaultOutboxConfig(),
		logging.NewNoopLogger(),
		newTestRegistry(t),
		newTestUpgraders(),
	)
	require.NoError(t, err)

	publishDone := make(chan error, 1)
	go func() { publishDone <- p.PublishPending(context.Background()) }()
	<-publishEntered

	setterStarted := make(chan struct{})
	setterDone := make(chan error, 1)
	go func() {
		close(setterStarted)
		setterDone <- p.SetEventRegistry(newTestRegistry(t))
	}()
	<-setterStarted

	select {
	case err := <-setterDone:
		t.Fatalf("SetEventRegistry completed during PublishPending: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	release()
	require.NoError(t, <-publishDone)
	require.NoError(t, <-setterDone)
}
