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
// 生命周期（如实描述）：清理 ctx 独立派生自 context.Background()（仅取传入
// ctx 的 Deadline 较小值作为超时上限，即 min(配置间隔, 默认上限)），因此这是
// 机会性清理——不随 Stop / loop 退出取消，Stop 后仍可能在超时上限内继续运行；
// 并发面由 cleanupMu.TryLock 约束（至多一个清理同时执行），未完成的删除由
// 下一轮 cleanupTicker 或下次发布成功后的清理继续推进，分批删除天然可续。
//
// 已知残留（审计 P2#11 fa43670f，未修，已列入发行说明未修清单）：该 goroutine
// 不受 supervisor 约束、无 panic 恢复——仓储 panic 会击穿进程。
func (p *Publisher[ID]) spawnCleanupAfterSuccessfulPublish(ctx context.Context) {
	go p.cleanupPublishedAfterSuccessfulPublish(ctx)
}

func (p *Publisher[ID]) cleanupPublishedAfterSuccessfulPublish(ctx context.Context) {
	if !p.cleanupMu.TryLock() {
		return
	}
	defer p.cleanupMu.Unlock()

	cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupAfterPublishTimeout(p.cfg.CleanupInterval))
	defer cancel()
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			// 机会性清理是发布路径的附带动作，不能被外部长超时反向延长
			//（慢查询/锁等待会长期占用连接）；取两者较小值。
			if remaining := time.Until(deadline); remaining > 0 && remaining < cleanupAfterPublishTimeout(p.cfg.CleanupInterval) {
				var ctxCancel context.CancelFunc
				cleanupCtx, ctxCancel = context.WithTimeout(context.Background(), remaining)
				defer ctxCancel()
			}
		}
	}
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
