package projection

import (
	"context"
	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
)

// TenantAwareProjector 是一个会按租户过滤事件的投影装饰器。
type TenantAwareProjector[ID comparable] struct {
	projector IProjection[ID]
}

// NewTenantAwareProjector 为现有投影包一层租户过滤逻辑。
func NewTenantAwareProjector[ID comparable](p IProjection[ID]) IProjection[ID] {
	return &TenantAwareProjector[ID]{
		projector: p,
	}
}

func (p *TenantAwareProjector[ID]) Name() string {
	return p.projector.Name()
}

// Handle 仅在事件租户与当前上下文租户匹配时才转发给底层投影。
func (p *TenantAwareProjector[ID]) Handle(ctx context.Context, event eventing.IEvent) error {
	// 提取当前租户 ID
	contextTenantID := contextx.TenantID(ctx)
	if contextTenantID == "" {
		// 无租户上下文，处理所有事件
		return p.projector.Handle(ctx, event)
	}

	eventTenantID := extractTenantIDFromMessage(event)

	// 只处理属于当前租户的事件
	if eventTenantID == contextTenantID {
		return p.projector.Handle(ctx, event)
	}

	// 跳过其他租户的事件
	return nil
}

// HandleWithCheckpoint 在 checkpoint 模式下继续复用租户过滤，并把原子边界交给底层投影。
func (p *TenantAwareProjector[ID]) HandleWithCheckpoint(ctx context.Context, event eventing.IEvent, store ICheckpointStore, checkpoint *Checkpoint) error {
	contextTenantID := contextx.TenantID(ctx)
	if contextTenantID == "" {
		cp, ok := p.projector.(ICheckpointingProjection[ID])
		if !ok {
			return errors.NewCode(errors.Unsupported, "tenant projector inner projection does not support checkpoint mode").
				WithContext("projection", p.projector.Name())
		}
		return cp.HandleWithCheckpoint(ctx, event, store, checkpoint)
	}

	eventTenantID := extractTenantIDFromMessage(event)
	if eventTenantID != contextTenantID {
		return saveSkippedCheckpoint(ctx, store, checkpoint)
	}

	cp, ok := p.projector.(ICheckpointingProjection[ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "tenant projector inner projection does not support checkpoint mode").
			WithContext("projection", p.projector.Name())
	}
	return cp.HandleWithCheckpoint(ctx, event, store, checkpoint)
}

func saveSkippedCheckpoint(ctx context.Context, store ICheckpointStore, checkpoint *Checkpoint) error {
	if store == nil || checkpoint == nil {
		return nil
	}
	if err := validateCheckpointTransaction(ctx, store); err != nil {
		return err
	}
	return store.Save(ctx, checkpoint)
}

func (p *TenantAwareProjector[ID]) SupportedEventTypes() []string {
	return p.projector.SupportedEventTypes()
}

func (p *TenantAwareProjector[ID]) Status() ProjectionStatus {
	return p.projector.Status()
}

// Rebuild 在重建时只把属于当前租户的事件批次传给底层投影。
func (p *TenantAwareProjector[ID]) Rebuild(ctx context.Context, events []eventing.Event[ID]) error {
	filteredEvents := p.filterTenantEvents(ctx, events)
	return p.projector.Rebuild(ctx, filteredEvents)
}

// RebuildWithCheckpoint 在 checkpoint 重建模式下继续复用租户过滤，并把原子边界交给底层投影。
func (p *TenantAwareProjector[ID]) RebuildWithCheckpoint(ctx context.Context, events []eventing.Event[ID], store ICheckpointStore, checkpoint *Checkpoint) error {
	cp, ok := p.projector.(IRebuildCheckpointingProjection[ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "tenant projector inner projection does not support rebuild checkpoint mode").
			WithContext("projection", p.projector.Name())
	}
	return cp.RebuildWithCheckpoint(ctx, p.filterTenantEvents(ctx, events), store, checkpoint)
}

func (p *TenantAwareProjector[ID]) filterTenantEvents(ctx context.Context, events []eventing.Event[ID]) []eventing.Event[ID] {
	// 过滤租户事件
	contextTenantID := contextx.TenantID(ctx)
	if contextTenantID == "" {
		// 无租户上下文，处理所有事件
		return events
	}

	// 过滤事件
	filteredEvents := make([]eventing.Event[ID], 0, len(events))
	for _, event := range events {
		eventTenantID := extractTenantIDFromEvent(&event)
		if eventTenantID == contextTenantID {
			filteredEvents = append(filteredEvents, event)
		}
	}
	return filteredEvents
}

// extractTenantIDFromEvent 从强类型事件对象里提取租户 ID。
func extractTenantIDFromEvent[ID comparable](event *eventing.Event[ID]) string {
	if event == nil {
		return ""
	}
	tenantID, _ := event.GetMetadata().GetString(contextx.MetadataTenantKey)
	return tenantID
}

// extractTenantIDFromMessage 从事件元数据里提取租户 ID。
func extractTenantIDFromMessage(event eventing.IEvent) string {
	if event == nil {
		return ""
	}
	if msg := event.GetMetadata(); msg != nil {
		tenantID, _ := msg.GetString(contextx.MetadataTenantKey)
		return tenantID
	}
	return ""
}
