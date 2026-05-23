package outbox

import (
	"context"
	"gochen/errors"
	"gochen/eventing/bus"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/logging"
	"sync"
	"time"
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

	stopCh chan struct{}
	doneCh chan struct{}

	mu        sync.Mutex
	started   bool
	stopped   bool
	runCancel context.CancelFunc
	stopOnce  sync.Once
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
	}, nil
}

// WithEventRegistry 替换发布时用于反序列化事件的注册表。
func (p *Publisher[ID]) WithEventRegistry(r *registry.Registry) *Publisher[ID] {
	if p == nil || r == nil {
		return p
	}
	p.eventRegistry = r
	return p
}

// WithUpgraderRegistry 替换事件升级链注册表。
func (p *Publisher[ID]) WithUpgraderRegistry(r *upcast.UpgraderRegistry) *Publisher[ID] {
	if p == nil || r == nil {
		return p
	}
	p.upgraders = r
	return p
}

// SetMetricsRecorder 注入发布过程的指标记录器。
func (p *Publisher[ID]) SetMetricsRecorder(recorder IPublisherMetricsRecorder) {
	p.metrics = recorder
}

// SetDLQRepository 为超过最大重试次数的记录启用 DLQ 转移。
func (p *Publisher[ID]) SetDLQRepository(dlq IDLQRepository[ID]) {
	p.dlq = dlq
}

func (p *Publisher[ID]) core() outboxPublisherCore[ID] {
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
	runCtx, cancel := context.WithCancel(ctx)
	p.runCancel = cancel
	p.mu.Unlock()

	go p.loop(runCtx)
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
		// Stop-before-Start：no-op（与 ParallelPublisher 对齐）。
		p.mu.Unlock()
		return nil
	}
	if p.stopped {
		p.mu.Unlock()
		return nil
	}
	p.started = false
	p.stopped = true
	cancel = p.runCancel
	p.runCancel = nil
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	p.stopOnce.Do(func() { close(p.stopCh) })
	select {
	case <-p.doneCh:
		return nil
	case <-ctx.Done():
		// best-effort stop：已触发取消/关闭信号；这里仅返回 ctx 错误表示“等待超时/取消”。
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

	p.mu.Lock()
	stopped := p.stopped
	p.mu.Unlock()
	if stopped {
		return errors.NewCode(errors.InvalidInput, "publisher has been stopped; create a new instance")
	}

	p.processMu.Lock()
	defer p.processMu.Unlock()
	return p.processOnce(ctx)
}

// loop 按配置周期重复执行单次发布与已发布记录清理。
func (p *Publisher[ID]) loop(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.PublishInterval)
	defer func() {
		ticker.Stop()

		// 无论是 Stop 还是 ctx.Done() 导致 loop 退出，均视为 terminal：不可再次 Start。
		// 这样可以避免 “ctx 结束后后台已停，但 started 仍为 true 导致后续 Start 被短路” 的歧义。
		p.mu.Lock()
		p.started = false
		p.stopped = true
		p.mu.Unlock()

		close(p.doneCh)
	}()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.processMu.Lock()
			err := p.processOnce(ctx)
			p.processMu.Unlock()
			if err != nil {
				p.log.Error(ctx, "outbox processOnce failed in loop", logging.Error(err))
			}
			// 定期清理已发布
			if err := p.core().cleanupPublished(ctx, time.Now()); err != nil {
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

	for _, e := range entries {
		result := p.core().publishClaimed(ctx, e)
		if result.keepaliveErr != nil {
			p.log.Error(ctx, "outbox claim keepalive failed",
				logging.Int64("entry", e.ID),
				logging.String("event_type", e.EventType),
				logging.Error(result.keepaliveErr))
			if firstErr == nil {
				firstErr = result.keepaliveErr
			}
			continue
		}
		if result.err != nil {
			p.handleEntryFailure(ctx, &firstErr, e, result.stage, result.err)
			continue
		}
		if !p.core().markPublishedAfterSuccessfulPublish(ctx, ClaimedEntry{ID: e.ID, ClaimToken: e.ClaimToken}, 2) {
			// 标记已发布失败不会影响事件已经成功发送到总线，但可能导致后续重复处理，
			// 因此仅记录错误日志，不作为本次批处理的致命错误返回。
			continue
		}
	}
	return firstErr
}

type outboxFailureStage uint8

const (
	outboxFailureDeserialize outboxFailureStage = iota
	outboxFailurePublish
)

// handleEntryFailure 统一处理发布失败后的标记、告警和可选 DLQ 转移。
func (p *Publisher[ID]) handleEntryFailure(
	ctx context.Context,
	firstErr *error,
	e OutboxEntry[ID],
	stage outboxFailureStage,
	err error,
	extraWarnFields ...logging.Field,
) {
	if p == nil {
		return
	}

	core := p.core()
	mark := core.buildFailureMark(e, err.Error())
	if markErr := core.markFailed(ctx, mark); markErr != nil {
		p.log.Error(ctx, stage.markFailedLogMessage(), logging.Int64("entry", e.ID), logging.Error(markErr))
		if firstErr != nil && *firstErr == nil {
			*firstErr = markErr
		}
		p.log.Warn(ctx, stage.warnLogMessage(), logging.Int64("entry", e.ID), logging.Error(err))
		return
	}

	warnFields := []logging.Field{
		logging.Int64("entry", e.ID),
		logging.Error(err),
	}
	warnFields = append(warnFields, extraWarnFields...)
	p.log.Warn(ctx, stage.warnLogMessage(), warnFields...)

	p.maybeMoveToDLQ(ctx, firstErr, e, stage, mark)
}

// maybeMoveToDLQ 在达到最大重试次数后尝试把记录转入 DLQ。
func (p *Publisher[ID]) maybeMoveToDLQ(ctx context.Context, firstErr *error, e OutboxEntry[ID], stage outboxFailureStage, mark outboxFailureMark[ID]) {
	if p == nil {
		return
	}

	if dlqErr := p.core().moveFailureToDLQ(ctx, mark); dlqErr != nil {
		p.log.Error(ctx, stage.moveToDLQLogMessage(), logging.Int64("entry", e.ID), logging.Error(dlqErr))
		if firstErr != nil && *firstErr == nil {
			*firstErr = dlqErr
		}
	}
}

// markFailedLogMessage 返回当前失败阶段对应的“标记失败”日志文案。
func (s outboxFailureStage) markFailedLogMessage() string {
	switch s {
	case outboxFailureDeserialize:
		return "outbox mark deserialize failed entry as failed"
	case outboxFailurePublish:
		return "outbox mark publish failed entry as failed"
	default:
		return "outbox mark failed entry as failed"
	}
}

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

// moveToDLQLogMessage 返回当前失败阶段对应的 DLQ 转移失败日志文案。
func (s outboxFailureStage) moveToDLQLogMessage() string {
	switch s {
	case outboxFailureDeserialize:
		return "outbox move to DLQ failed after deserialize error"
	case outboxFailurePublish:
		return "outbox move to DLQ failed after publish error"
	default:
		return "outbox move to DLQ failed"
	}
}
