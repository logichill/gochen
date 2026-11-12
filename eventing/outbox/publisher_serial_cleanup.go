package outbox

import (
	"context"
	"time"

	"gochen/observe/logging"
)

func (p *Publisher[ID]) cleanupPublished(ctx context.Context, now time.Time) error {
	p.cleanupMu.Lock()
	defer p.cleanupMu.Unlock()
	return p.core().cleanupPublished(ctx, now)
}

// spawnCleanupAfterSuccessfulPublish 在独立 goroutine 中执行发布后清理，
// 避免分批删除耗时阻塞持有 processMu 的发布循环。
//
// 清理保持发布身份、租户与取消传播，由实际任务注册表管理并参与 Stop 等待。
func (p *Publisher[ID]) spawnCleanupAfterSuccessfulPublish(ctx context.Context) {
	if err := p.tasks.registry.Go(ctx, p.tasks.prefix+"cleanup_after_publish"); err != nil {
		p.log.Error(ctx, "start Outbox post-publish cleanup failed", logging.Error(err))
	}
}

func (p *Publisher[ID]) cleanupPublishedAfterSuccessfulPublish(ctx context.Context) {
	if !p.cleanupMu.TryLock() {
		return
	}
	defer p.cleanupMu.Unlock()

	cleanupCtx, cancel := context.WithTimeout(ctx, cleanupAfterPublishTimeout(p.cfg.CleanupInterval))
	defer cancel()
	if err := p.core().cleanupPublished(cleanupCtx, time.Now()); err != nil {
		p.log.Error(ctx, "outbox delete published after successful publish failed", logging.Error(err))
	}
}

func cleanupAfterPublishTimeout(interval time.Duration) time.Duration {
	if interval > 0 && interval < defaultParallelPublisherStopTimeout {
		return interval
	}
	return defaultParallelPublisherStopTimeout
}
