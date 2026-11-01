package eventstore

import (
	"context"
	"fmt"

	"gochen/app/internal/depcheck"
	"gochen/contextx"
	"gochen/domain"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
)

// AppendEvents 按构造时选定的发布模式追加领域事件。
// PersistOnly 仅保存；Direct 保存后发布，发布失败返回 AfterCommitError；
// Outbox 原子保存事件与待发布记录，后台 Publisher 负责后续发布。
func (a *domainEventStore[ID]) AppendEvents(ctx context.Context, aggregateID ID, events []domain.IDomainEvent, expectedVersion uint64) error {
	if len(events) == 0 {
		return nil
	}

	storableEvents, err := a.prepareDomainEvents(ctx, aggregateID, events, expectedVersion)
	if err != nil {
		return err
	}
	needDirectPublish := a.publishMode == PublishModeDirect
	var publishedEvents []eventing.IEvent
	if needDirectPublish {
		publishedEvents = make([]eventing.IEvent, len(storableEvents))
		for i := range storableEvents {
			publishedEvents[i] = &storableEvents[i]
		}
	}

	// Outbox 模式：通过 OutboxRepo 原子保存事件与 Outbox。
	if a.publishMode == PublishModeOutbox {
		if err := a.outboxRepo.SaveWithEvents(ctx, aggregateID, storableEvents); err != nil {
			return err
		}
	} else {
		// 直接写入 EventStore。
		storable := store.ToStorable(storableEvents)
		if err := a.eventStore.AppendEvents(ctx, a.aggregateType, aggregateID, storable, expectedVersion); err != nil {
			return err
		}
	}

	// 可选事件发布（仅在非 Outbox 模式下直接发布）。
	if needDirectPublish {
		if err := a.eventBus.PublishEvents(ctx, publishedEvents); err != nil {
			// 发布失败视为可观测的业务失败，向上游返回错误并保留持久化结果
			return contextx.WrapAfterCommitError(errors.Wrap(err, errors.Dependency, "failed to publish domain events"))
		}
	}

	return nil
}

func (a *domainEventStore[ID]) prepareDomainEvents(ctx context.Context, aggregateID ID, events []domain.IDomainEvent, expectedVersion uint64) ([]eventing.Event[ID], error) {
	storableEvents := make([]eventing.Event[ID], 0, len(events))
	for i, de := range events {
		if depcheck.IsNil(de) {
			return nil, errors.NewCode(errors.InvalidInput, "domain event cannot be nil").
				WithContext("index", i)
		}
		eventType := de.EventType()
		if eventType == "" {
			return nil, errors.NewCode(errors.InvalidInput, "domain event type cannot be empty").
				WithContext("domain_event_type", fmt.Sprintf("%T", de))
		}
		if !a.eventRegistry.HasEvent(eventType) {
			return nil, errors.NewCode(errors.NotFound, "unknown event type").
				WithContext("event_type", eventType).
				WithContext("aggregate_type", a.aggregateType).
				WithContext("aggregate_id", aggregateID).
				WithContext("index", i)
		}
		schemaVersion := a.eventRegistry.EventSchemaVersion(eventType)
		version := expectedVersion + uint64(i) + 1
		evt, err := eventing.NewEvent(a.eventIDGenerator, aggregateID, a.aggregateType, eventType, version, de, schemaVersion)
		if err != nil {
			return nil, errors.Wrap(err, errors.Dependency, "generate event envelope failed").
				WithContext("aggregate_type", a.aggregateType).
				WithContext("aggregate_id", aggregateID).
				WithContext("event_type", eventType).
				WithContext("index", i)
		}
		// 事件信封不变量校验：与底层存储（memory/SQL）的入队校验同口径，
		// 让 Outbox 模式在写入前也能拦截非法信封（如数值聚合 ID 非正）。
		if err := evt.Validate(); err != nil {
			return nil, errors.Wrap(err, errors.InvalidInput, "domain event envelope validation failed").
				WithContext("event_type", eventType).
				WithContext("aggregate_id", aggregateID).
				WithContext("index", i)
		}
		if err := contextx.InjectEventAudit(ctx, evt.GetMetadata()); err != nil {
			return nil, err
		}
		storableEvents = append(storableEvents, *evt)
	}

	return storableEvents, nil
}
