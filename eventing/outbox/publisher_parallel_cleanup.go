package outbox

import (
	"context"
	"time"

	"gochen/logging"
)

// cleanupLoop 定期清理已发布的记录。
func (p *ParallelPublisher[ID]) cleanupLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.cfg.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			if err := p.core().cleanupPublished(ctx, time.Now()); err != nil {
				p.log.Error(ctx, "cleanup published failed", logging.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}
