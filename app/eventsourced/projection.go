package eventsourced

import (
	"context"
	"sync"
	"time"

	gerrors "gochen/errors"
	"gochen/eventing"
	"gochen/eventing/projection"
	"gochen/logging"
)

// EventSourcedProjectionOption 投影配置。
type EventSourcedProjectionOption[T any] struct {
	Name       string
	EventTypes []string
	Decoder    func(event eventing.IEvent) (T, error)
	Handle     func(ctx context.Context, payload T) error
	Rebuild    func(ctx context.Context, events []eventing.Event[int64]) error
	Logger     logging.ILogger
}

// EventSourcedProjection 泛型投影模板。
type EventSourcedProjection[T any] struct {
	name       string
	eventTypes []string
	decoder    func(event eventing.IEvent) (T, error)
	handle     func(ctx context.Context, payload T) error
	rebuild    func(ctx context.Context, events []eventing.Event[int64]) error
	logger     logging.ILogger

	statusMu sync.RWMutex
	status   projection.ProjectionStatus
}

// NewEventSourcedProjection 创建事件Sourced投影。
func NewEventSourcedProjection[T any](opt EventSourcedProjectionOption[T]) (*EventSourcedProjection[T], error) {
	if opt.Name == "" {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "projection name cannot be empty")
	}
	if len(opt.EventTypes) == 0 {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "projection must subscribe at least one event type")
	}
	if opt.Handle == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "projection handle function cannot be nil")
	}

	decoder := opt.Decoder
	if decoder == nil {
		decoder = defaultPayloadDecoder[T]()
	}

	p := &EventSourcedProjection[T]{
		name:       opt.Name,
		eventTypes: opt.EventTypes,
		decoder:    decoder,
		handle:     opt.Handle,
		rebuild:    opt.Rebuild,
		logger:     opt.Logger,
		status: projection.ProjectionStatus{
			Name:      opt.Name,
			Status:    "stopped",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	if p.logger == nil {
		p.logger = logging.ComponentLogger("app.eventsourced.projection").
			WithField("projection", opt.Name)
	}
	return p, nil
}

// GetName 投影名称。
func (p *EventSourcedProjection[T]) Name() string {
	return p.name
}

// Handle 处理消息并执行业务处理逻辑。
//
// 说明：
// - Handle 处理事件。
//
// 参数：
// - evt：事件数据。
func (p *EventSourcedProjection[T]) Handle(ctx context.Context, evt eventing.IEvent) error {
	if evt == nil {
		return gerrors.NewCode(gerrors.InvalidInput, "event cannot be nil")
	}
	payload, err := p.decoder(evt)
	if err != nil {
		p.logger.Error(ctx, "failed to decode event",
			logging.Error(err),
			logging.String("event_id", evt.GetID()),
			logging.String("event_type", evt.GetType()))
		return err
	}
	if err := p.handle(ctx, payload); err != nil {
		p.logger.Error(ctx, "projection handle failed",
			logging.Error(err),
			logging.String("event_id", evt.GetID()),
			logging.String("event_type", evt.GetType()))
		p.updateStatusError(evt, err)
		return err
	}
	p.updateStatusSuccess(evt)
	return nil
}

func (p *EventSourcedProjection[T]) SupportedEventTypes() []string {
	return append([]string{}, p.eventTypes...)
}

// Rebuild 重建投影。
func (p *EventSourcedProjection[T]) Rebuild(ctx context.Context, events []eventing.Event[int64]) error {
	if p.rebuild != nil {
		return p.rebuild(ctx, events)
	}
	for _, evt := range events {
		if err := p.Handle(ctx, &evt); err != nil {
			return err
		}
	}
	return nil
}

// GetStatus 投影状态。
func (p *EventSourcedProjection[T]) Status() projection.ProjectionStatus {
	p.statusMu.RLock()
	defer p.statusMu.RUnlock()
	return p.status
}

// updateStatusSuccess 更新状态Success。
func (p *EventSourcedProjection[T]) updateStatusSuccess(evt eventing.IEvent) {
	p.statusMu.Lock()
	defer p.statusMu.Unlock()
	p.status.LastEventID = evt.GetID()
	p.status.LastEventTime = evt.GetTimestamp()
	p.status.ProcessedEvents++
	p.status.Status = "running"
	p.status.LastError = ""
	p.status.UpdatedAt = time.Now()
}

// updateStatusError 更新状态错误。
func (p *EventSourcedProjection[T]) updateStatusError(evt eventing.IEvent, err error) {
	p.statusMu.Lock()
	defer p.statusMu.Unlock()
	p.status.LastEventID = evt.GetID()
	p.status.LastEventTime = evt.GetTimestamp()
	p.status.FailedEvents++
	p.status.Status = "error"
	p.status.LastError = err.Error()
	p.status.UpdatedAt = time.Now()
}

// Ensure interface compliance.
var _ projection.IProjection[int64] = (*EventSourcedProjection[any])(nil)
