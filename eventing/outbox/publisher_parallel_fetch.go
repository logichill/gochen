package outbox

import (
	"context"
	"time"

	"gochen/errors"
	"gochen/observe/logging"
)

// fetchLoop 主循环，定期拉取待发布记录。
func (p *ParallelPublisher[ID]) fetchLoop(ctx context.Context) {
	defer func() {
		p.dispatchMu.Lock()
		p.closeWorkChannels()
		p.dispatchMu.Unlock()
		p.wg.Done()
	}()

	ticker := time.NewTicker(p.cfg.PublishInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			func() {
				p.dispatchMu.Lock()
				defer p.dispatchMu.Unlock()
				_ = p.fetchOnce(ctx)
			}()
		case <-ctx.Done():
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
			drainCtx, cancel := p.claimedDrainContext(ctx)
			p.drainClaimedEntriesToWorkers(drainCtx, entries[i:])
			cancel()
			return nil
		case <-ctx.Done():
			drainCtx, cancel := p.claimedDrainContext(ctx)
			p.drainClaimedEntriesToWorkers(drainCtx, entries[i:])
			cancel()
			return ctx.Err()
		}
	}

	return nil
}

func (p *ParallelPublisher[ID]) claimedDrainContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx != nil && ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.Background(), defaultParallelPublisherStopTimeout)
}

func (p *ParallelPublisher[ID]) drainClaimedEntriesToWorkers(ctx context.Context, entries []OutboxEntry[ID]) {
	for _, entry := range entries {
		select {
		case p.workChs[p.shardIndex(entry)] <- entry:
		case <-ctx.Done():
			return
		}
	}
}
