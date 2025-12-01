package outbox

import (
	"context"
	"time"

	"gochen/eventing/bus"
	"gochen/logging"
)

// Publisher 实现 IOutboxPublisher，按批拉取未发布记录并发布到事件总线
type Publisher struct {
	repo IOutboxRepository
	bus  bus.IEventBus
	cfg  OutboxConfig
	log  logging.ILogger

	// 可选：DLQ 仓储，用于超过最大重试次数后的迁移
	dlq IDLQRepository

	stopCh chan struct{}
	doneCh chan struct{}
}

func NewPublisher(repo IOutboxRepository, bus bus.IEventBus, cfg OutboxConfig, logger logging.ILogger) *Publisher {
	if logger == nil {
		logger = logging.ComponentLogger("eventing.outbox.publisher")
	}
	return &Publisher{repo: repo, bus: bus, cfg: cfg, log: logger, stopCh: make(chan struct{}), doneCh: make(chan struct{})}
}

// SetDLQRepository 设置 DLQ 仓储（可选）
func (p *Publisher) SetDLQRepository(dlq IDLQRepository) {
	p.dlq = dlq
}

func (p *Publisher) Start(ctx context.Context) error {
	go p.loop(ctx)
	return nil
}

func (p *Publisher) Stop() error {
	close(p.stopCh)
	<-p.doneCh
	return nil
}

func (p *Publisher) PublishPending(ctx context.Context) error {
	return p.processOnce(ctx)
}

func (p *Publisher) loop(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.PublishInterval)
	defer func() { ticker.Stop(); close(p.doneCh) }()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			_ = p.processOnce(ctx)
			// 定期清理已发布
			_ = p.repo.DeletePublished(ctx, time.Now().Add(-p.cfg.RetentionPeriod))
		case <-ctx.Done():
			return
		}
	}
}

func (p *Publisher) processOnce(ctx context.Context) error {
	entries, err := p.repo.GetPendingEntries(ctx, p.cfg.BatchSize)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	for _, e := range entries {
		evt, err := e.ToEvent()
		if err != nil {
			// 无法反序列化，直接标记失败并设置下次重试
			next := e.CalculateNextRetryTime(p.cfg.RetryInterval)
			_ = p.repo.MarkAsFailed(ctx, e.ID, err.Error(), next)
			p.log.Warn(ctx, "outbox deserialize failed", logging.Int64("entry", e.ID), logging.Error(err))
			// 超过最大重试次数则移入 DLQ（若已配置）
			if p.dlq != nil && e.RetryCount+1 >= p.cfg.MaxRetries {
				// 复制一份并更新重试次数，以反映最新状态
				ee := e
				ee.RetryCount = e.RetryCount + 1
				_ = p.dlq.MoveToDLQ(ctx, ee)
			}
			continue
		}
		if err := p.bus.PublishEvent(ctx, &evt); err != nil {
			next := e.CalculateNextRetryTime(p.cfg.RetryInterval)
			_ = p.repo.MarkAsFailed(ctx, e.ID, err.Error(), next)
			p.log.Warn(ctx, "outbox publish failed", logging.Int64("entry", e.ID), logging.Error(err))
			if p.dlq != nil && e.RetryCount+1 >= p.cfg.MaxRetries {
				ee := e
				ee.RetryCount = e.RetryCount + 1
				_ = p.dlq.MoveToDLQ(ctx, ee)
			}
			continue
		}
		if err := p.repo.MarkAsPublished(ctx, e.ID); err != nil {
			p.log.Warn(ctx, "outbox mark published failed", logging.Int64("entry", e.ID), logging.Error(err))
		}
	}
	return nil
}
