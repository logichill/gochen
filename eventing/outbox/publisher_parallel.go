package outbox

import (
	"context"
	"sync"
	"time"

	"gochen/eventing/bus"
	"gochen/logging"
)

// ParallelPublisher 并行发布器
//
// 使用 worker pool 模式并行处理 Outbox 记录，提升发布吞吐量。
//
// 特性：
//   - 可配置 worker 数量
//   - 任务分发通过 channel
//   - 优雅关闭
//   - 并发安全
//
// 使用示例：
//
//	publisher := NewParallelPublisher(repo, bus, cfg, logger, 5) // 5 个 worker
//	err := publisher.Start(ctx)
//	defer publisher.Stop()
type ParallelPublisher struct {
	repo        IOutboxRepository
	bus         bus.IEventBus
	cfg         OutboxConfig
	log         logging.ILogger
	workerCount int

	// 可选：DLQ 仓储
	dlq IDLQRepository

	workCh chan OutboxEntry
	stopCh chan struct{}
	wg     sync.WaitGroup

	mu      sync.Mutex
	started bool
}

// NewParallelPublisher 创建并行发布器
//
// workerCount 指定并行处理的 worker 数量，建议值：
//   - CPU 密集型：CPU 核数
//   - IO 密集型：CPU 核数 * 2 ~ 4
func NewParallelPublisher(
	repo IOutboxRepository,
	bus bus.IEventBus,
	cfg OutboxConfig,
	logger logging.ILogger,
	workerCount int,
) *ParallelPublisher {
	if logger == nil {
		logger = logging.ComponentLogger("eventing.outbox.parallel_publisher")
	}
	if workerCount <= 0 {
		workerCount = 1
	}

	return &ParallelPublisher{
		repo:        repo,
		bus:         bus,
		cfg:         cfg,
		log:         logger,
		workerCount: workerCount,
		workCh:      make(chan OutboxEntry, workerCount*2), // 缓冲区为 worker 数量的 2 倍
		stopCh:      make(chan struct{}),
	}
}

// SetDLQRepository 设置 DLQ 仓储（可选）
func (p *ParallelPublisher) SetDLQRepository(dlq IDLQRepository) { p.dlq = dlq }

// Start 启动并行发布器
func (p *ParallelPublisher) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = true
	p.mu.Unlock()

	// 启动 worker pool
	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}

	// 启动主循环（拉取任务）
	p.wg.Add(1)
	go p.fetchLoop(ctx)

	// 启动清理任务
	p.wg.Add(1)
	go p.cleanupLoop(ctx)

	return nil
}

// Stop 停止并行发布器
//
// 等待所有正在处理的任务完成后关闭。
func (p *ParallelPublisher) Stop() error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	close(p.stopCh)
	p.wg.Wait()

	p.mu.Lock()
	p.started = false
	p.mu.Unlock()

	return nil
}

// PublishPending 手动触发发布待处理的事件
func (p *ParallelPublisher) PublishPending(ctx context.Context) error {
	return p.fetchOnce(ctx)
}

// fetchLoop 主循环，定期拉取待发布记录
func (p *ParallelPublisher) fetchLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.cfg.PublishInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			// 优雅关闭：关闭 workCh，等待 worker 处理完
			close(p.workCh)
			return
		case <-ticker.C:
			_ = p.fetchOnce(ctx)
		case <-ctx.Done():
			close(p.workCh)
			return
		}
	}
}

// fetchOnce 拉取一批待发布记录并分发给 worker
func (p *ParallelPublisher) fetchOnce(ctx context.Context) error {
	entries, err := p.repo.GetPendingEntries(ctx, p.cfg.BatchSize)
	if err != nil {
		p.log.Error(ctx, "fetch pending entries failed", logging.Error(err))
		return err
	}

	if len(entries) == 0 {
		return nil
	}

	p.log.Debug(ctx, "fetched pending entries", logging.Int("count", len(entries)))

	// 分发任务到 worker
	for _, entry := range entries {
		select {
		case p.workCh <- entry:
		case <-p.stopCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// worker 处理单个 Outbox 记录
func (p *ParallelPublisher) worker(ctx context.Context, id int) {
	defer p.wg.Done()

	p.log.Debug(ctx, "worker started", logging.Int("worker_id", id))

	for {
		select {
		case entry, ok := <-p.workCh:
			if !ok {
				// channel 已关闭
				p.log.Debug(ctx, "worker stopped", logging.Int("worker_id", id))
				return
			}
			p.processEntry(ctx, entry)
		case <-p.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// processEntry 处理单个 Outbox 记录
func (p *ParallelPublisher) processEntry(ctx context.Context, entry OutboxEntry) {
	// 反序列化事件
	evt, err := entry.ToEvent()
	if err != nil {
		p.log.Warn(ctx, "deserialize event failed",
			logging.Int64("entry_id", entry.ID),
			logging.Error(err))
		p.markFailed(ctx, entry, err.Error())
		return
	}

	// 发布事件
	if err := p.bus.PublishEvent(ctx, &evt); err != nil {
		p.log.Warn(ctx, "publish event failed",
			logging.Int64("entry_id", entry.ID),
			logging.String("event_type", entry.EventType),
			logging.Error(err))
		p.markFailed(ctx, entry, err.Error())
		return
	}

	// 标记为已发布
	if err := p.repo.MarkAsPublished(ctx, entry.ID); err != nil {
		p.log.Warn(ctx, "mark as published failed",
			logging.Int64("entry_id", entry.ID),
			logging.Error(err))
		return
	}

	p.log.Debug(ctx, "event published successfully",
		logging.Int64("entry_id", entry.ID),
		logging.String("event_type", entry.EventType))
}

// markFailed 标记记录为失败
func (p *ParallelPublisher) markFailed(ctx context.Context, entry OutboxEntry, errorMsg string) {
	nextRetryAt := entry.CalculateNextRetryTime(p.cfg.RetryInterval)
	if err := p.repo.MarkAsFailed(ctx, entry.ID, errorMsg, nextRetryAt); err != nil {
		p.log.Error(ctx, "mark as failed failed",
			logging.Int64("entry_id", entry.ID),
			logging.Error(err))
	}

	// 超过最大重试次数则移入 DLQ（若已配置）
	if p.dlq != nil && entry.RetryCount+1 >= p.cfg.MaxRetries {
		ee := entry
		ee.RetryCount = entry.RetryCount + 1
		if err := p.dlq.MoveToDLQ(ctx, ee); err != nil {
			p.log.Error(ctx, "move to DLQ failed",
				logging.Int64("entry_id", entry.ID),
				logging.Error(err))
		}
	}
}

// cleanupLoop 定期清理已发布的记录
func (p *ParallelPublisher) cleanupLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.cfg.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			olderThan := time.Now().Add(-p.cfg.RetentionPeriod)
			if err := p.repo.DeletePublished(ctx, olderThan); err != nil {
				p.log.Error(ctx, "cleanup published failed", logging.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}
