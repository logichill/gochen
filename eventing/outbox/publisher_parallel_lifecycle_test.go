package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/logging"
)

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
			require.True(t, errors.Is(err, errors.InvalidInput), "expected INVALID_INPUT after ctx.Done, got: %v", err)
			require.True(t, errors.Is(p.PublishPending(context.Background()), errors.InvalidInput))
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("expected ctx.Done to make publisher terminal (Start should eventually fail)")
}
