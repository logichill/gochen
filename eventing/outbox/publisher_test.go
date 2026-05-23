package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/logging"
)

// TestPublisher_PublishPending_PublishesAllEntries 验证串行 Publisher 可通过多次 PublishPending 完整发布所有 pending 记录。
func TestPublisher_PublishPending_PublishesAllEntries(t *testing.T) {
	const entryCount = 120

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
		BatchSize:       25,
		RetryInterval:   30 * time.Second,
		RetentionPeriod: time.Minute,
		MaxRetries:      3,
	}

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	p, err := NewPublisher(repo, bus, cfg, logging.NewNoopLogger(), reg, upgraders)
	require.NoError(t, err)

	ctx := context.Background()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = p.PublishPending(ctx)

		pub, failed := repo.stats()
		if failed != 0 {
			t.Fatalf("expected no failed entries, got %d", failed)
		}
		if pub >= entryCount {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	pub, failed := repo.stats()
	require.Equal(t, 0, failed)
	require.Equal(t, entryCount, pub)
	require.Equal(t, int32(entryCount), bus.count)

	// 验证每条 entry 只被标记发布一次（避免重复 publish+mark）。
	repo.mu.Lock()
	defer repo.mu.Unlock()
	for i := 1; i <= entryCount; i++ {
		require.Equal(t, 1, repo.published[int64(i)], "entry should be marked published exactly once")
		require.Equal(t, OutboxStatusPublished, repo.entries[int64(i)].Status)
	}
}
