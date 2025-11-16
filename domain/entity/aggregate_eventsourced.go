// Package entity 定义事件溯源聚合根实现
package entity

import (
	"sync"

	"gochen/eventing"
)

// IEventSourcedAggregate 事件溯源聚合根接口
// 使用事件溯源模式的聚合根，状态完全由事件重建
type IEventSourcedAggregate[T comparable] interface {
	IAggregate[T]

	// ApplyEvent 应用事件到聚合根（修改状态）
	// 此方法应该是幂等的
	ApplyEvent(evt eventing.IEvent) error

	// GetUncommittedEvents 获取未提交的事件
	GetUncommittedEvents() []eventing.IEvent

	// MarkEventsAsCommitted 标记事件为已提交
	MarkEventsAsCommitted()

	// LoadFromHistory 从事件历史重建状态
	LoadFromHistory(events []eventing.IEvent) error
}

// EventSourcedAggregate 事件溯源聚合根（泛型实现）
//
// 使用场景:
//   - 完全的事件溯源模式
//   - 状态通过重放事件重建
//   - 支持任意 ID 类型（int64、string、UUID 等）
//   - 并发安全的事件管理
//
// 示例:
//
//	type BankAccount struct {
//	    *EventSourcedAggregate[string]
//	    Balance int
//	}
//
//	func (a *BankAccount) ApplyEvent(evt event.IEvent) error {
//	    switch evt.GetType() {
//	    case "MoneyDeposited":
//	        a.Balance += evt.GetPayload().(int)
//	    }
//	    return a.EventSourcedAggregate.ApplyEvent(evt)
//	}
type EventSourcedAggregate[T comparable] struct {
	id                T
	version           uint64
	uncommittedEvents []eventing.IEvent
	aggregateType     string
	mu                sync.RWMutex // 并发保护
}

// NewEventSourcedAggregate 创建事件溯源聚合根
func NewEventSourcedAggregate[T comparable](id T, aggregateType string) *EventSourcedAggregate[T] {
	return &EventSourcedAggregate[T]{
		id:                id,
		version:           0,
		uncommittedEvents: make([]eventing.IEvent, 0),
		aggregateType:     aggregateType,
	}
}

// GetID 实现 IObject 接口
func (a *EventSourcedAggregate[T]) GetID() T {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.id
}

// GetVersion 实现 IEntity 接口
func (a *EventSourcedAggregate[T]) GetVersion() int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return int64(a.version)
}

// GetAggregateType 实现 IAggregate 接口
func (a *EventSourcedAggregate[T]) GetAggregateType() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.aggregateType
}

// GetDomainEvents 实现 IAggregate 接口
func (a *EventSourcedAggregate[T]) GetDomainEvents() []eventing.IEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()
	// 返回副本以保证并发安全
	events := make([]eventing.IEvent, len(a.uncommittedEvents))
	copy(events, a.uncommittedEvents)
	return events
}

// ClearDomainEvents 实现 IAggregate 接口
func (a *EventSourcedAggregate[T]) ClearDomainEvents() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.uncommittedEvents = nil
}

// AddDomainEvent 实现 IAggregate 接口
func (a *EventSourcedAggregate[T]) AddDomainEvent(evt eventing.IEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.uncommittedEvents == nil {
		a.uncommittedEvents = make([]eventing.IEvent, 0)
	}
	a.uncommittedEvents = append(a.uncommittedEvents, evt)
}

// GetUncommittedEvents 实现 IEventSourcedAggregate 接口
func (a *EventSourcedAggregate[T]) GetUncommittedEvents() []eventing.IEvent {
	return a.GetDomainEvents() // 复用并发安全的实现
}

// MarkEventsAsCommitted 实现 IEventSourcedAggregate 接口
func (a *EventSourcedAggregate[T]) MarkEventsAsCommitted() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.uncommittedEvents = nil
}

// ApplyEvent 实现 IEventSourcedAggregate 接口（需要子类重写）
//
// 此方法是一个钩子，子类应该重写它来处理具体的事件：
//
//	func (a *MyAggregate) ApplyEvent(evt event.IEvent) error {
//	    switch evt.GetType() {
//	    case "SomethingHappened":
//	        // 更新聚合状态
//	        a.SomeField = evt.GetPayload().(SomeType)
//	    }
//	    // 调用基类方法更新版本号
//	    return a.EventSourcedAggregate.ApplyEvent(evt)
//	}
func (a *EventSourcedAggregate[T]) ApplyEvent(evt eventing.IEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	// 子类需要重写此方法来应用具体的事件
	a.version++
	return nil
}

// LoadFromHistory 实现 IEventSourcedAggregate 接口
//
// 从事件历史重建聚合状态，通常在从 EventStore 加载聚合时使用
func (a *EventSourcedAggregate[T]) LoadFromHistory(events []eventing.IEvent) error {
	for _, evt := range events {
		if err := a.ApplyEvent(evt); err != nil {
			return err
		}
	}
	return nil
}

// Validate 实现 IValidatable 接口（默认实现）
func (a *EventSourcedAggregate[T]) Validate() error {
	return nil
}

// ApplyAndRecord 应用事件并记录为未提交
//
// 这是命令处理器应该调用的方法：
//  1. 先应用事件（更新状态）
//  2. 再记录事件（准备持久化）
//
// 示例:
//
//	evt := NewMoneyDepositedEvent(accountID, amount)
//	if err := account.ApplyAndRecord(evt); err != nil {
//	    return err
//	}
func (a *EventSourcedAggregate[T]) ApplyAndRecord(evt eventing.IEvent) error {
	if err := a.ApplyEvent(evt); err != nil {
		return err
	}
	a.AddDomainEvent(evt)
	return nil
}

// RestoreSnapshotMeta 由快照管理器调用，用于从快照元信息恢复聚合基础字段
// 注意：仅当快照管理器提供 int64 类型的 ID 时才会设置 ID（本项目聚合 ID 均为 int64）
func (a *EventSourcedAggregate[T]) RestoreSnapshotMeta(id int64, aggregateType string, version uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v, ok := any(id).(T); ok {
		a.id = v
	}
	a.aggregateType = aggregateType
	a.version = version
}
