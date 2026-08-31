package outbox

import (
	"context"
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

// Publisher 负责轮询 Outbox、发布事件并回写发布结果。
type Publisher[ID comparable] struct {
	repo    IOutboxRepository[ID]
	bus     bus.IEventBus
	cfg     OutboxConfig
	log     logging.ILogger
	metrics IPublisherMetricsRecorder

	eventRegistry *registry.Registry
	upgraders     *upcast.UpgraderRegistry

	// 可选：DLQ 仓储，用于超过最大重试次数后的迁移
	dlq IDLQRepository[ID]

	// processMu 串行化单次发布流程，避免 loop 与 PublishPending 并发触发导致重复发布。
	processMu sync.Mutex
	// cleanupMu 串行化清理流程，并允许发布后清理在 timer 正在运行时跳过。
	cleanupMu sync.Mutex

	stopCh   chan struct{}
	doneCh   chan struct{}
	stopDone chan struct{}

	mu           sync.Mutex
	started      bool
	stopped      bool
	runCancel    context.CancelFunc
	stopOnce     sync.Once
	stopDoneOnce sync.Once
	// coreSnapshot 在 Start 后冻结；Start 前 PublishPending 仍按当前依赖即时构造。
	coreSnapshot outboxPublisherCore[ID]
	coreReady    atomic.Uint32
	supervisor   *task.TaskSupervisor
}

// NewPublisher 创建一个串行发布的 Outbox publisher。
func NewPublisher[ID comparable](
	repo IOutboxRepository[ID],
	bus bus.IEventBus,
	cfg OutboxConfig,
	logger logging.ILogger,
	reg *registry.Registry,
	upgraders *upcast.UpgraderRegistry,
) (*Publisher[ID], error) {
	if logger == nil {
		logger = logging.ComponentLogger("eventing.outbox.publisher")
	}
	if err := validatePublisherDependencies(repo, bus); err != nil {
		return nil, err
	}
	if err := validatePublisherCodecs(reg, upgraders); err != nil {
		return nil, err
	}
	normalizedCfg, err := normalizeOutboxConfigForRepository(cfg, repo)
	if err != nil {
		return nil, err
	}
	return &Publisher[ID]{
		repo:          repo,
		bus:           bus,
		cfg:           normalizedCfg,
		log:           logger,
		eventRegistry: reg,
		upgraders:     upgraders,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		stopDone:      make(chan struct{}),
		supervisor:    task.NewTaskSupervisorWithLogger("eventing.outbox.publisher", logger),
	}, nil
}

// SetEventRegistry 配置发布时用于反序列化事件的注册表。
func (p *Publisher[ID]) SetEventRegistry(r *registry.Registry) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	if r == nil {
		return errors.NewCode(errors.InvalidInput, "event registry cannot be nil")
	}
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConfigurableLocked(); err != nil {
		return err
	}
	p.eventRegistry = r
	return nil
}

// SetUpgraderRegistry 配置事件升级链注册表。
func (p *Publisher[ID]) SetUpgraderRegistry(r *upcast.UpgraderRegistry) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	if r == nil {
		return errors.NewCode(errors.InvalidInput, "event upgrader registry cannot be nil")
	}
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConfigurableLocked(); err != nil {
		return err
	}
	p.upgraders = r
	return nil
}

// SetMetricsRecorder 注入发布过程的指标记录器。
func (p *Publisher[ID]) SetMetricsRecorder(recorder IPublisherMetricsRecorder) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConfigurableLocked(); err != nil {
		return err
	}
	p.metrics = recorder
	return nil
}

// SetDLQRepository 为超过最大重试次数的记录启用 DLQ 转移。
func (p *Publisher[ID]) SetDLQRepository(dlq IDLQRepository[ID]) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConfigurableLocked(); err != nil {
		return err
	}
	p.dlq = dlq
	return nil
}

func (p *Publisher[ID]) core() outboxPublisherCore[ID] {
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

func (p *Publisher[ID]) snapshotCoreLocked() outboxPublisherCore[ID] {
	if p.supervisor == nil {
		p.supervisor = task.NewTaskSupervisorWithLogger("eventing.outbox.publisher", p.log)
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

func (p *Publisher[ID]) freezeCoreLocked() {
	p.coreSnapshot = p.snapshotCoreLocked()
	p.coreReady.Store(1)
}

func (p *Publisher[ID]) ensureConfigurableLocked() error {
	if p.started || p.stopped {
		return errors.NewCode(errors.FailedPrecondition, "publisher runtime options cannot be changed after start")
	}
	return nil
}

// Start 启动后台轮询循环；同一个 publisher 只允许启动并停止一次。
func (p *Publisher[ID]) Start(ctx context.Context) error {
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
	p.freezeCoreLocked()
	runCtx, cancel := context.WithCancel(ctx)
	p.runCancel = cancel
	if p.supervisor == nil {
		p.supervisor = task.NewTaskSupervisorWithLogger("eventing.outbox.publisher", p.log)
	}
	p.mu.Unlock()

	if err := p.supervisor.Go(runCtx, "poll_loop", func(taskCtx context.Context) {
		p.loop(taskCtx)
	}); err != nil {
		cancel()
		p.mu.Lock()
		p.started = false
		p.runCancel = nil
		p.coreReady.Store(0)
		p.mu.Unlock()
		return errors.Wrap(err, errors.Internal, "start outbox publisher loop failed")
	}
	return nil
}

// Stop 请求后台循环退出，并等待当前一次发布流程收尾。
func (p *Publisher[ID]) Stop(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	var cancel context.CancelFunc
	p.mu.Lock()
	if !p.started && !p.stopped {
		p.mu.Unlock()
		p.processMu.Lock()
		p.mu.Lock()
		stopped := p.stopped
		p.mu.Unlock()
		p.processMu.Unlock()
		if stopped {
			return p.waitForStop(ctx)
		}
		return nil
	}
	if p.stopped {
		cancel = p.runCancel
		p.runCancel = nil
		p.mu.Unlock()
	} else {
		p.started = false
		p.stopped = true
		cancel = p.runCancel
		p.runCancel = nil
		p.mu.Unlock()
	}

	if cancel != nil {
		cancel()
	}
	p.stopOnce.Do(func() { close(p.stopCh) })
	if err := p.waitForStop(ctx); err != nil {
		return err
	}
	if p.supervisor != nil {
		return p.supervisor.Stop(ctx)
	}
	return nil
}

func (p *Publisher[ID]) waitForStop(ctx context.Context) error {
	select {
	case <-p.stopDone:
		return nil
	default:
	}
	select {
	case <-p.stopDone:
		return nil
	case <-ctx.Done():
		select {
		case <-p.stopDone:
			return nil
		default:
		}
		if ctx.Err() == context.DeadlineExceeded {
			return errors.NewCode(errors.Timeout, "outbox publisher stop timeout").WithContext("cause", ctx.Err().Error())
		}
		return ctx.Err()
	}
}

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
	if err == nil {
		p.cleanupPublishedAfterSuccessfulPublish(ctx)
	}
	return err
}

// loop 按配置周期重复执行单次发布，并按清理周期删除已发布记录。
func (p *Publisher[ID]) loop(ctx context.Context) {
	publishTicker := time.NewTicker(p.cfg.PublishInterval)
	cleanupTicker := time.NewTicker(p.cfg.CleanupInterval)
	defer func() {
		publishTicker.Stop()
		cleanupTicker.Stop()

		p.mu.Lock()
		p.started = false
		p.stopped = true
		cancel := p.runCancel
		p.runCancel = nil
		p.mu.Unlock()
		if cancel != nil {
			cancel()
		}

		close(p.doneCh)
		p.processMu.Lock()
		p.stopDoneOnce.Do(func() { close(p.stopDone) })
		p.processMu.Unlock()
	}()
	for {
		select {
		case <-p.stopCh:
			return
		case <-publishTicker.C:
			p.processMu.Lock()
			err := p.processOnce(ctx)
			if err == nil {
				p.cleanupPublishedAfterSuccessfulPublish(ctx)
			}
			p.processMu.Unlock()
			if err != nil {
				p.log.Error(ctx, "outbox processOnce failed in loop", logging.Error(err))
			}
		case <-cleanupTicker.C:
			if err := p.cleanupPublished(ctx, time.Now()); err != nil {
				p.log.Error(ctx, "outbox delete published failed", logging.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}

// processOnce 处理一批待发布记录，并返回首个需要上报的错误。
func (p *Publisher[ID]) processOnce(ctx context.Context) error {
	var firstErr error

	entries, err := p.core().claimPending(ctx)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	if err := validatePublisherCodecs(p.eventRegistry, p.upgraders); err != nil {
		return err
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
	return firstErr
}

func (p *Publisher[ID]) cleanupPublished(ctx context.Context, now time.Time) error {
	p.cleanupMu.Lock()
	defer p.cleanupMu.Unlock()
	return p.core().cleanupPublished(ctx, now)
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
			if remaining := time.Until(deadline); remaining > 0 {
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

type outboxFailureStage uint8

const (
	outboxFailureDeserialize outboxFailureStage = iota
	outboxFailurePublish
)

func (s outboxFailureStage) warnLogMessage() string {
	switch s {
	case outboxFailureDeserialize:
		return "outbox deserialize failed"
	case outboxFailurePublish:
		return "outbox publish failed"
	default:
		return "outbox entry failed"
	}
}
