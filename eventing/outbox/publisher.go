package outbox

import (
	"context"
	"sync"
	"time"

	"gochen/eventing/bus"
	"gochen/logging"
)

// Publisher 实现 IOutboxPublisher，按批拉取未发布记录并发布到事件总线
type Publisher struct {
	repo IOutboxRepository[int64]
	bus  bus.IEventBus
	cfg  OutboxConfig
	log  logging.ILogger

	// 可选：DLQ 仓储，用于超过最大重试次数后的迁移
	dlq IDLQRepository

	stopCh chan struct{}
	doneCh chan struct{}

	stopOnce sync.Once
}

func NewPublisher(repo IOutboxRepository[int64], bus bus.IEventBus, cfg OutboxConfig, logger logging.ILogger) *Publisher {
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
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})
	<-p.doneCh
	return nil
}

// Close 实现关闭语义，便于作为资源统一管理
func (p *Publisher) Close() error {
	return p.Stop()
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
			if err := p.processOnce(ctx); err != nil {
				p.log.Error(ctx, "outbox processOnce failed in loop", logging.Error(err))
			}
			// 定期清理已发布
			if err := p.repo.DeletePublished(ctx, time.Now().Add(-p.cfg.RetentionPeriod)); err != nil {
				p.log.Error(ctx, "outbox delete published failed", logging.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}

func (p *Publisher) processOnce(ctx context.Context) error {
	var firstErr error

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
			if markErr := p.repo.MarkAsFailed(ctx, e.ID, err.Error(), next); markErr != nil {
				p.log.Error(ctx, "outbox mark deserialize failed entry as failed", logging.Int64("entry", e.ID), logging.Error(markErr))
				if firstErr == nil {
					firstErr = markErr
				}
			}
			p.log.Warn(ctx, "outbox deserialize failed", logging.Int64("entry", e.ID), logging.Error(err))
			// 超过最大重试次数则移入 DLQ（若已配置）
			if p.dlq != nil && e.RetryCount+1 >= p.cfg.MaxRetries {
				// 复制一份并更新重试次数，以反映最新状态
				ee := e
				ee.RetryCount = e.RetryCount + 1
				if dlqErr := p.dlq.MoveToDLQ(ctx, ee); dlqErr != nil {
					p.log.Error(ctx, "outbox move to DLQ failed after deserialize error", logging.Int64("entry", e.ID), logging.Error(dlqErr))
					if firstErr == nil {
						firstErr = dlqErr
					}
				}
			}
			continue
		}
		if err := p.bus.PublishEvent(ctx, &evt); err != nil {
			next := e.CalculateNextRetryTime(p.cfg.RetryInterval)
			if markErr := p.repo.MarkAsFailed(ctx, e.ID, err.Error(), next); markErr != nil {
				p.log.Error(ctx, "outbox mark publish failed entry as failed", logging.Int64("entry", e.ID), logging.Error(markErr))
				if firstErr == nil {
					firstErr = markErr
				}
			}
			p.log.Warn(ctx, "outbox publish failed", logging.Int64("entry", e.ID), logging.Error(err))
			if p.dlq != nil && e.RetryCount+1 >= p.cfg.MaxRetries {
				ee := e
				ee.RetryCount = e.RetryCount + 1
				if dlqErr := p.dlq.MoveToDLQ(ctx, ee); dlqErr != nil {
					p.log.Error(ctx, "outbox move to DLQ failed after publish error", logging.Int64("entry", e.ID), logging.Error(dlqErr))
					if firstErr == nil {
						firstErr = dlqErr
					}
				}
			}
			continue
		}
		if err := p.repo.MarkAsPublished(ctx, e.ID); err != nil {
			// 标记已发布失败不会影响事件已经成功发送到总线，但可能导致后续重复处理，
			// 因此仅记录错误日志，不作为本次批处理的致命错误返回。
			p.log.Error(ctx, "outbox mark published failed", logging.Int64("entry", e.ID), logging.Error(err))
		}
	}
	return firstErr
}
