package outbox

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"gochen/errors"
	"gochen/eventing/bus"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/observe/logging"
	"gochen/process/task"
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
	markStopCh chan struct{}
	stopCh     chan struct{}
	dispatchMu sync.Mutex
	markSendMu sync.Mutex
	workClosed bool // 受 dispatchMu 保护：worker 输入通道是否已关闭
	wg         sync.WaitGroup

	mu               sync.Mutex
	started          bool
	stopped          bool
	stopOnce         sync.Once // 保护 stopCh 只关闭一次
	workChOnce       sync.Once // 保护 workChs 只关闭一次
	markChOnce       sync.Once // 保护 markCh 只关闭一次
	markStopOnce     sync.Once // 保护 markStopCh 只关闭一次
	markDoneCh       chan struct{}
	stopDone         chan struct{}
	markStopCtx      context.Context
	markStopped      bool
	runCancel        context.CancelFunc
	stopFinalizeOnce sync.Once
	// coreSnapshot 在 Start 后冻结，供热路径无锁复用（启动前依赖字段仍可变）。
	coreSnapshot outboxPublisherCore[ID]
	coreReady    atomic.Uint32
	supervisor   *task.TaskSupervisor
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
		stopDone:      make(chan struct{}),
		supervisor:    task.NewTaskSupervisorWithLogger("eventing.outbox.parallel_publisher", logger),
	}, nil
}

// SetDLQRepository 为超过最大重试次数的记录启用 DLQ 转移。
func (p *ParallelPublisher[ID]) SetDLQRepository(dlq IDLQRepository[ID]) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConfigurableLocked(); err != nil {
		return err
	}
	p.dlq = dlq
	return nil
}

// SetMetricsRecorder 注入发布过程的指标记录器。
func (p *ParallelPublisher[ID]) SetMetricsRecorder(recorder IPublisherMetricsRecorder) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConfigurableLocked(); err != nil {
		return err
	}
	p.metrics = recorder
	return nil
}

// SetBatchOperations 启用批量标记能力，以减少发布结果回写时的数据库往返。
func (p *ParallelPublisher[ID]) SetBatchOperations(batchOps IBatchRepository) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConfigurableLocked(); err != nil {
		return err
	}
	p.batchOps = batchOps
	return nil
}

func (p *ParallelPublisher[ID]) ensureConfigurableLocked() error {
	if p.started || p.stopped {
		return errors.NewCode(errors.FailedPrecondition, "publisher runtime options cannot be changed after start")
	}
	return nil
}

func (p *ParallelPublisher[ID]) core() outboxPublisherCore[ID] {
	if p.coreReady.Load() == 1 {
		return p.coreSnapshot
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.coreReady.Load() == 1 {
		return p.coreSnapshot
	}
	return p.snapshotCoreLocked()
}

func (p *ParallelPublisher[ID]) snapshotCoreLocked() outboxPublisherCore[ID] {
	if p.supervisor == nil {
		p.supervisor = task.NewTaskSupervisorWithLogger("eventing.outbox.parallel_publisher", p.log)
	}
	return outboxPublisherCore[ID]{
		repo:          p.repo,
		bus:           p.bus,
		cfg:           p.cfg,
		log:           p.log,
		metrics:       p.metrics,
		supervisor:    p.supervisor,
		eventRegistry: p.eventRegistry,
		upgraders:     p.upgraders,
		dlq:           p.dlq,
	}
}

func (p *ParallelPublisher[ID]) freezeCoreLocked() {
	p.coreSnapshot = p.snapshotCoreLocked()
	p.coreReady.Store(1)
}

// closeWorkChannels 关闭所有 worker 输入通道，通知其退出消费循环。
// 调用方必须持有 dispatchMu；关闭后置位 workClosed，供 PublishPending 复查停止状态。
func (p *ParallelPublisher[ID]) closeWorkChannels() {
	p.workChOnce.Do(func() {
		for i := range p.workChs {
			close(p.workChs[i])
		}
	})
	p.workClosed = true
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
	runCtx, runCancel := context.WithCancel(ctx)
	batchOps := p.batchOps
	var markCh chan markOp[ID]
	var markStopCh chan struct{}
	var markDoneCh chan struct{}
	if batchOps != nil {
		markCap := p.workerCount * p.cfg.BatchSize
		if markCap < p.workerCount*4 {
			markCap = p.workerCount * 4
		}
		if markCap > 10000 {
			markCap = 10000
		}
		markCh = make(chan markOp[ID], markCap)
		markStopCh = make(chan struct{})
		markDoneCh = make(chan struct{})
		p.markCh = markCh
		p.markStopCh = markStopCh
		p.markDoneCh = markDoneCh
		p.markStopped = false
	}
	p.wg.Add(p.workerCount + 2)
	p.started = true
	p.runCancel = runCancel
	p.freezeCoreLocked()
	if p.supervisor == nil {
		p.supervisor = task.NewTaskSupervisorWithLogger("eventing.outbox.parallel_publisher", p.log)
	}

	trackedLaunches := 0
	markLoopLaunched := false
	launchTracked := func(name string, fn func(context.Context)) error {
		if err := p.supervisor.Go(runCtx, name, fn); err != nil {
			return err
		}
		trackedLaunches++
		return nil
	}
	abortStart := func(startErr error) error {
		for i := trackedLaunches; i < p.workerCount+2; i++ {
			p.wg.Done()
		}
		if !markLoopLaunched {
			p.markDoneCh = nil
		}
		p.started = false
		p.stopped = true
		p.runCancel = nil
		p.stopOnce.Do(func() { close(p.stopCh) })
		runCancel()
		p.mu.Unlock()
		p.dispatchMu.Lock()
		p.closeWorkChannels()
		p.dispatchMu.Unlock()
		p.stopFinalizeOnce.Do(func() { go p.finishStop(nil) })
		return errors.Wrap(startErr, errors.Internal, "start outbox parallel publisher task failed")
	}

	if markCh != nil && markDoneCh != nil {
		if err := p.supervisor.Go(runCtx, "mark_loop", func(taskCtx context.Context) {
			p.markLoop(taskCtx)
		}); err != nil {
			return abortStart(err)
		}
		markLoopLaunched = true
	}
	for i := 0; i < p.workerCount; i++ {
		workerID := i
		if err := launchTracked(fmt.Sprintf("worker_%d", workerID), func(taskCtx context.Context) {
			p.worker(taskCtx, workerID, p.workChs[workerID])
		}); err != nil {
			return abortStart(err)
		}
	}
	if err := launchTracked("fetch_loop", func(taskCtx context.Context) {
		p.fetchLoop(taskCtx)
	}); err != nil {
		return abortStart(err)
	}
	if err := launchTracked("cleanup_loop", func(taskCtx context.Context) {
		p.cleanupLoop(taskCtx)
	}); err != nil {
		return abortStart(err)
	}
	if err := p.supervisor.Go(runCtx, "context_watcher", func(taskCtx context.Context) {
		select {
		case <-taskCtx.Done():
			p.requestStopFromRunContext()
		case <-p.stopCh:
			return
		}
	}); err != nil {
		return abortStart(err)
	}

	p.mu.Unlock()
	return nil
}

func (p *ParallelPublisher[ID]) requestStopFromRunContext() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	runCancel := p.runCancel
	p.started = false
	p.stopped = true
	p.mu.Unlock()

	p.stopOnce.Do(func() { close(p.stopCh) })
	p.stopFinalizeOnce.Do(func() { go p.finishStop(runCancel) })
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
	firstStop := !p.stopped
	runCancel := p.runCancel
	if firstStop {
		if p.markCh != nil {
			p.markStopCtx = ctx
		}
		p.started = false
		p.stopped = true
	}
	stopDone := p.stopDone
	p.mu.Unlock()

	if firstStop {
		p.stopOnce.Do(func() { close(p.stopCh) })
		p.stopFinalizeOnce.Do(func() { go p.finishStop(runCancel) })
	}

	select {
	case <-stopDone:
		return nil
	default:
	}
	select {
	case <-stopDone:
		return nil
	case <-ctx.Done():
		select {
		case <-stopDone:
			return nil
		default:
		}
		if runCancel != nil {
			runCancel()
		}
		p.stopMarkLoop()
		if ctx.Err() == context.DeadlineExceeded {
			return errors.NewCode(errors.Timeout, "outbox parallel publisher stop timeout").WithContext("cause", ctx.Err().Error())
		}
		return ctx.Err()
	}
}

func (p *ParallelPublisher[ID]) finishStop(runCancel context.CancelFunc) {
	// fetchLoop closes worker inputs; workers drain claimed entries before final mark flush.
	p.wg.Wait()
	if p.markCh != nil {
		p.markChOnce.Do(func() { close(p.markCh) })
	}
	if p.markDoneCh != nil {
		<-p.markDoneCh
	}
	if runCancel != nil {
		runCancel()
	}
	if p.supervisor != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), defaultParallelPublisherStopTimeout)
		if err := p.supervisor.Stop(stopCtx); err != nil {
			p.log.Error(stopCtx, "stop parallel publisher task supervisor failed", logging.Error(err))
		}
		cancel()
	}
	p.mu.Lock()
	p.runCancel = nil
	p.mu.Unlock()
	close(p.stopDone)
}

func (p *ParallelPublisher[ID]) stopMarkLoop() {
	if p == nil {
		return
	}
	p.markSendMu.Lock()
	p.mu.Lock()
	p.markStopped = true
	stopCh := p.markStopCh
	p.mu.Unlock()
	if stopCh != nil {
		p.markStopOnce.Do(func() {
			close(stopCh)
		})
	}
	p.markSendMu.Unlock()
}

func (p *ParallelPublisher[ID]) tryEnqueueMark(ctx context.Context, op markOp[ID]) bool {
	if p == nil || ctx == nil || ctx.Err() != nil {
		return false
	}
	p.markSendMu.Lock()
	defer p.markSendMu.Unlock()
	p.mu.Lock()
	batchOps := p.batchOps
	markCh := p.markCh
	stopped := p.markStopped
	p.mu.Unlock()
	if batchOps == nil || markCh == nil || stopped {
		return false
	}
	select {
	case markCh <- op:
		return true
	default:
		return false
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
		return errors.NewCode(errors.FailedPrecondition, "publisher is not started")
	}

	p.dispatchMu.Lock()
	defer p.dispatchMu.Unlock()
	// dispatchMu 同时被 fetchLoop 的 closeWorkChannels 持有：在锁内复查 workClosed，
	// 避免与 Stop/ctx 取消竞争时在已关闭的 workCh/markCh 上发送导致 panic。
	if p.workClosed {
		return errors.NewCode(errors.FailedPrecondition, "publisher is not started")
	}
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
