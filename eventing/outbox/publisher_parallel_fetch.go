package outbox

import (
	"context"
	"time"

	"gochen/errors"
	"gochen/logging"
)

// fetchLoop 主循环，定期拉取待发布记录。
func (p *ParallelPublisher[ID]) fetchLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.cfg.PublishInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			// 优雅关闭：关闭 workCh，等待 worker drain 完成后退出。
			p.dispatchMu.Lock()
			p.closeWorkChannels()
			p.dispatchMu.Unlock()
			return
		case <-ticker.C:
			p.dispatchMu.Lock()
			_ = p.fetchOnce(ctx)
			p.dispatchMu.Unlock()
		case <-ctx.Done():
			p.dispatchMu.Lock()
			p.closeWorkChannels()
			p.dispatchMu.Unlock()
			return
		}
	}
}

// fetchOnce 拉取一批待发布记录并分发给 worker。
func (p *ParallelPublisher[ID]) fetchOnce(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := validatePublisherDependencies(p.repo, p.bus); err != nil {
		return err
	}

	entries, err := p.core().claimPending(ctx)
	if err != nil {
		p.log.Error(ctx, "fetch pending entries failed", logging.Error(err))
		return err
	}

	if len(entries) == 0 {
		return nil
	}

	p.log.Debug(ctx, "fetched pending entries", logging.Int("count", len(entries)))

	// 分发任务到 worker
	for i, entry := range entries {
		ch := p.workChs[p.shardIndex(entry)]
		select {
		case ch <- entry:
		case <-p.stopCh:
			p.processClaimedEntriesDuringShutdown(entries[i:])
			return nil
		case <-ctx.Done():
			p.processClaimedEntriesDuringShutdown(entries[i:])
			return ctx.Err()
		}
	}

	return nil
}

func (p *ParallelPublisher[ID]) processClaimedEntriesDuringShutdown(entries []OutboxEntry[ID]) {
	if len(entries) == 0 {
		return
	}
	processCtx, cancel := context.WithTimeout(context.Background(), defaultParallelPublisherStopTimeout)
	defer cancel()
	for _, entry := range entries {
		if processCtx.Err() != nil {
			return
		}
		p.processEntry(processCtx, entry)
	}
}
