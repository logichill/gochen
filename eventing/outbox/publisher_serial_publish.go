package outbox

import (
	"context"

	"gochen/errors"
)

// PublishPending 立即触发一次待发布记录扫描与发布。
func (p *Publisher[ID]) PublishPending(ctx context.Context) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := validatePublisherDependencies(p.repo, p.bus); err != nil {
		return err
	}

	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.mu.Lock()
	stopped := p.stopped
	p.mu.Unlock()
	if stopped {
		return errors.NewCode(errors.InvalidInput, "publisher has been stopped; create a new instance")
	}
	err := p.processOnce(ctx)
	if err == nil && p.lastProcessPublished {
		p.cleanupPublishedAfterSuccessfulPublish(ctx)
	}
	return err
}

// processOnce 处理一批待发布记录，并返回首个需要上报的错误。
//
// 副作用：将本次是否真的发布过记录写入 p.lastProcessPublished。
// 调用方据此决定是否触发发布后清理：空闲轮询（无待发布记录）时
// 不应下发无 LIMIT 的 DeletePublished，否则空闲期会周期性产生
// 无效全表扫描与锁竞争。
func (p *Publisher[ID]) processOnce(ctx context.Context) error {
	p.lastProcessPublished = false
	var firstErr error

	// 编码器校验必须先于 claimPending：先锁定记录再发现注册表异常，会让
	// 本批记录悬挂在 PROCESSING 状态（租约默认 5 分钟）无法被消费或重试。
	if err := validatePublisherCodecs(p.eventRegistry, p.upgraders); err != nil {
		return err
	}

	entries, err := p.core().claimPending(ctx)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	core := p.core()
	strategy := outboxMarkStrategy[ID]{
		markPublished: core.markPublishedWithRecovery,
		markFailed:    core.markFailureDirect,
	}
	for _, e := range entries {
		if err := core.processClaimed(ctx, e, strategy); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// 只要本批确实拿走并处理过记录，就认为有发布动作发生；
	// 即便部分失败，已发布的那部分仍需清理。
	p.lastProcessPublished = true
	return firstErr
}
