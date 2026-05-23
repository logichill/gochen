package outbox

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"gochen/errors"
	"gochen/eventing/bus"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/logging"
)

// ParallelPublisher 使用 worker pool 并行发布 Outbox 记录，以提升吞吐量。
type ParallelPublisher[ID comparable] struct {
	repo        IOutboxRepository[ID]
	bus         bus.IEventBus
	cfg         OutboxConfig
	log         logging.ILogger
	metrics     IPublisherMetricsRecorder
	workerCount int

	eventRegistry *registry.Registry
	upgraders     *upcast.UpgraderRegistry

	// 可选：DLQ 仓储
	dlq IDLQRepository[ID]
	// 可选：批量标记操作（用于减少 MarkAsPublished/MarkAsFailed 的 DB 往返）
	batchOps IBatchRepository

	workChs    []chan OutboxEntry[ID]
	markCh     chan markOp[ID]
	stopCh     chan struct{}
	dispatchMu sync.Mutex
	wg         sync.WaitGroup

	mu         sync.Mutex
	started    bool
	stopped    bool
	stopOnce   sync.Once // 保护 stopCh 只关闭一次
	workChOnce sync.Once // 保护 workChs 只关闭一次
	markChOnce sync.Once // 保护 markCh 只关闭一次
	markDoneCh chan struct{}
}

// NewParallelPublisher 创建一个并行版 Outbox publisher。
func NewParallelPublisher[ID comparable](
	repo IOutboxRepository[ID],
	bus bus.IEventBus,
	cfg OutboxConfig,
	logger logging.ILogger,
	workerCount int,
	reg *registry.Registry,
	upgraders *upcast.UpgraderRegistry,
) (*ParallelPublisher[ID], error) {
	if logger == nil {
		logger = logging.ComponentLogger("eventing.outbox.parallel_publisher")
	}
	if err := validatePublisherDependencies(repo, bus); err != nil {
		return nil, err
	}
	if err := validatePublisherCodecs(reg, upgraders); err != nil {
		return nil, err
	}
	if workerCount <= 0 {
		workerCount = 1
	}
	normalizedCfg, err := normalizeOutboxConfigForRepository(cfg, repo)
	if err != nil {
		return nil, err
	}
	cfg = normalizedCfg

	bufPerWorker := cfg.BatchSize
	if bufPerWorker <= 0 {
		bufPerWorker = 100
	}
	if bufPerWorker < 2 {
		bufPerWorker = 2
	}
	if bufPerWorker > 1000 {
		bufPerWorker = 1000
	}
	workChs := make([]chan OutboxEntry[ID], workerCount)
	for i := 0; i < workerCount; i++ {
		workChs[i] = make(chan OutboxEntry[ID], bufPerWorker)
	}

	return &ParallelPublisher[ID]{
		repo:          repo,
		bus:           bus,
		cfg:           cfg,
		log:           logger,
		workerCount:   workerCount,
		eventRegistry: reg,
		upgraders:     upgraders,
		workChs:       workChs,
		stopCh:        make(chan struct{}),
	}, nil
}

// SetDLQRepository 为超过最大重试次数的记录启用 DLQ 转移。
func (p *ParallelPublisher[ID]) SetDLQRepository(dlq IDLQRepository[ID]) { p.dlq = dlq }

// SetMetricsRecorder 注入发布过程的指标记录器。
func (p *ParallelPublisher[ID]) SetMetricsRecorder(recorder IPublisherMetricsRecorder) {
	p.metrics = recorder
}

// SetBatchOperations 启用批量标记能力，以减少发布结果回写时的数据库往返。
func (p *ParallelPublisher[ID]) SetBatchOperations(batchOps IBatchRepository) { p.batchOps = batchOps }

func (p *ParallelPublisher[ID]) core() outboxPublisherCore[ID] {
	return outboxPublisherCore[ID]{
		repo:          p.repo,
		bus:           p.bus,
		cfg:           p.cfg,
		log:           p.log,
		metrics:       p.metrics,
		eventRegistry: p.eventRegistry,
		upgraders:     p.upgraders,
		dlq:           p.dlq,
	}
}

// closeWorkChannels 关闭所有 worker 输入通道，通知其退出消费循环。
func (p *ParallelPublisher[ID]) closeWorkChannels() {
	p.workChOnce.Do(func() {
		for i := range p.workChs {
			close(p.workChs[i])
		}
	})
}

// Start 启动 worker、拉取循环和清理循环。
func (p *ParallelPublisher[ID]) Start(ctx context.Context) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := validatePublisherDependencies(p.repo, p.bus); err != nil {
		return err
	}

	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return errors.NewCode(errors.InvalidInput, "publisher has been stopped; create a new instance")
	}
	if p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = true
	p.mu.Unlock()

	// 当 ctx 被取消时，自动进入停止流程并标记为 terminal，避免出现“后台已停但 Start 被短路”的歧义。
	// 同时避免在调用方 Stop 但 ctx 仍存活时泄漏 goroutine：stopCh 关闭后 watcher 会自动退出。
	go func() {
		select {
		case <-ctx.Done():
			stopCtx, stopCancel := context.WithTimeout(context.Background(), defaultParallelPublisherStopTimeout)
			defer stopCancel()
			_ = p.Stop(stopCtx)
		case <-p.stopCh:
			return
		}
	}()

	// 启动 worker pool
	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i, p.workChs[i])
	}

	// 启动主循环（拉取任务）
	p.wg.Add(1)
	go p.fetchLoop(ctx)

	// 启动清理任务
	p.wg.Add(1)
	go p.cleanupLoop(ctx)

	// 启用批量标记时，启动异步标记聚合器。
	if p.batchOps != nil {
		markCap := p.workerCount * p.cfg.BatchSize
		if markCap < p.workerCount*4 {
			markCap = p.workerCount * 4
		}
		if markCap > 10000 {
			markCap = 10000
		}
		p.markCh = make(chan markOp[ID], markCap)
		p.markDoneCh = make(chan struct{})
		go p.markLoop(ctx)
	}

	return nil
}

// Stop 请求并行发布器停止，并等待 worker 与批量标记协程收尾。
func (p *ParallelPublisher[ID]) Stop(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	p.mu.Lock()
	if !p.started && !p.stopped {
		p.mu.Unlock()
		return nil
	}
	if p.stopped {
		p.mu.Unlock()
		return nil
	}
	// 提前标记为未启动，避免并发 Stop 造成重复关闭 stopCh
	p.started = false
	p.stopped = true
	p.mu.Unlock()

	// 使用 sync.Once 确保 stopCh 只关闭一次
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})
	done := make(chan struct{})
	go func() {
		p.wg.Wait()

		// 所有 worker 已退出，安全关闭 markCh 并等待批量标记 goroutine flush 完成。
		if p.markCh != nil {
			p.markChOnce.Do(func() {
				close(p.markCh)
			})
		}
		if p.markDoneCh != nil {
			<-p.markDoneCh
		}

		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return errors.NewCode(errors.Timeout, "outbox parallel publisher stop timeout").WithContext("cause", ctx.Err().Error())
		}
		return ctx.Err()
	}
}

// PublishPending 手动触发一次待发布事件的抓取与分发。
func (p *ParallelPublisher[ID]) PublishPending(ctx context.Context) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := validatePublisherDependencies(p.repo, p.bus); err != nil {
		return err
	}

	p.mu.Lock()
	started := p.started
	p.mu.Unlock()
	if !started {
		return errors.NewCode(errors.InvalidInput, "publisher is not started")
	}

	p.dispatchMu.Lock()
	defer p.dispatchMu.Unlock()
	return p.fetchOnce(ctx)
}

// shardIndex 根据聚合类型和聚合 ID 计算记录应进入的 worker 分片。
func (p *ParallelPublisher[ID]) shardIndex(entry OutboxEntry[ID]) int {
	if p.workerCount <= 1 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(entry.AggregateType))
	_, _ = h.Write([]byte{0})

	switch v := any(entry.AggregateID).(type) {
	case string:
		_, _ = h.Write([]byte(v))
	case int64:
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], uint64(v))
		_, _ = h.Write(b[:])
	case uint64:
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], v)
		_, _ = h.Write(b[:])
	case int:
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], uint64(v))
		_, _ = h.Write(b[:])
	case uint:
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], uint64(v))
		_, _ = h.Write(b[:])
	default:
		_, _ = fmt.Fprintf(h, "%v", v)
	}

	return int(h.Sum32() % uint32(p.workerCount))
}

const defaultParallelPublisherStopTimeout = 30 * time.Second
