package eventsourced

import (
	"sync"

	"gochen/domain"
)

// IEventSourcedAggregate 事件溯源聚合根接口。
// 使用事件溯源模式的聚合根，状态完全由事件重建。
type IEventSourcedAggregate[T comparable] interface {
	domain.IEntity[T]

	// GetAggregateType 返回聚合根类型名称。
	GetAggregateType() string

	// ApplyEvent 应用事件到聚合根（修改状态），应为幂等。
	ApplyEvent(evt domain.IDomainEvent) error

	// GetUncommittedEvents 获取未提交的事件。
	GetUncommittedEvents() []domain.IDomainEvent

	// MarkEventsAsCommitted 标记事件为已提交。
	MarkEventsAsCommitted()
}

// EventSourcedAggregate 事件溯源聚合根（泛型实现）。
type EventSourcedAggregate[T comparable] struct {
	id                T
	version           uint64
	uncommittedEvents []domain.IDomainEvent
	aggregateType     string
	mu                sync.RWMutex
}

// NewEventSourcedAggregate 创建事件溯源聚合根。
func NewEventSourcedAggregate[T comparable](id T, aggregateType string) *EventSourcedAggregate[T] {
	return &EventSourcedAggregate[T]{
		id:                id,
		version:           0,
		uncommittedEvents: make([]domain.IDomainEvent, 0),
		aggregateType:     aggregateType,
	}
}

// GetID 实现 IObject 接口。
func (a *EventSourcedAggregate[T]) GetID() T {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.id
}

// GetVersion 实现 IEntity 接口。
func (a *EventSourcedAggregate[T]) GetVersion() int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return int64(a.version)
}

// GetAggregateType 返回聚合类型。
func (a *EventSourcedAggregate[T]) GetAggregateType() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.aggregateType
}

// GetDomainEvents 返回未提交事件的副本。
func (a *EventSourcedAggregate[T]) GetDomainEvents() []domain.IDomainEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()
	events := make([]domain.IDomainEvent, len(a.uncommittedEvents))
	copy(events, a.uncommittedEvents)
	return events
}

// ClearDomainEvents 清空未提交事件。
func (a *EventSourcedAggregate[T]) ClearDomainEvents() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.uncommittedEvents = nil
}

// AddDomainEvent 添加领域事件。
func (a *EventSourcedAggregate[T]) AddDomainEvent(evt domain.IDomainEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.uncommittedEvents == nil {
		a.uncommittedEvents = make([]domain.IDomainEvent, 0)
	}
	a.uncommittedEvents = append(a.uncommittedEvents, evt)
}

// GetUncommittedEvents 实现 IEventSourcedAggregate。
func (a *EventSourcedAggregate[T]) GetUncommittedEvents() []domain.IDomainEvent {
	return a.GetDomainEvents()
}

// MarkEventsAsCommitted 实现 IEventSourcedAggregate。
func (a *EventSourcedAggregate[T]) MarkEventsAsCommitted() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.uncommittedEvents = nil
}

// ApplyEvent 默认实现：仅递增版本号，具体聚合应在外部重写并调用此方法。
func (a *EventSourcedAggregate[T]) ApplyEvent(evt domain.IDomainEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.version++
	return nil
}

// Validate 默认实现：留给具体聚合覆盖。
func (a *EventSourcedAggregate[T]) Validate() error {
	return nil
}

// ApplyAndRecord 应用事件并记录为未提交。
func (a *EventSourcedAggregate[T]) ApplyAndRecord(evt domain.IDomainEvent) error {
	if err := a.ApplyEvent(evt); err != nil {
		return err
	}
	a.AddDomainEvent(evt)
	return nil
}
