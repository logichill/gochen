package outbox

import (
	"context"
	"sync"
	"sync/atomic"

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

	// lastProcessPublished 记录最近一次 processOnce 是否真的发布过记录，
	// 由 processMu 保护。空闲轮询时不得触发发布后清理。
	lastProcessPublished bool
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
