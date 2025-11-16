// Package entity 定义聚合根接口与基础实现
package entity

import (
	"gochen/eventing"
)

// IAggregate 聚合根接口
// 聚合根是业务一致性边界，负责维护业务规则
type IAggregate[T comparable] interface {
	IEntity[T]
	IValidatable

	// GetAggregateType 返回聚合根类型名称
	GetAggregateType() string

	// GetDomainEvents 获取未发布的领域事件
	GetDomainEvents() []eventing.IEvent

	// ClearDomainEvents 清空领域事件
	ClearDomainEvents()

	// AddDomainEvent 添加领域事件
	AddDomainEvent(evt eventing.IEvent)
}

// Aggregate 基础聚合根（支持领域事件）
// 适用于传统 CRUD + 领域事件模式
//
// 使用场景:
//   - 不需要事件溯源，只需要发布领域事件
//   - 状态通过传统 CRUD 持久化
//   - 事件仅用于通知其他聚合或服务
//
// 示例:
//
//	type User struct {
//	    Aggregate[int64]
//	    Name  string
//	    Email string
//	}
type Aggregate[T comparable] struct {
	EntityFields
	domainEvents []eventing.IEvent
}

// GetAggregateType 返回聚合根类型
func (a *Aggregate[T]) GetAggregateType() string {
	return "Aggregate"
}

// GetDomainEvents 获取领域事件
func (a *Aggregate[T]) GetDomainEvents() []eventing.IEvent {
	return a.domainEvents
}

// ClearDomainEvents 清空领域事件
func (a *Aggregate[T]) ClearDomainEvents() {
	a.domainEvents = nil
}

// AddDomainEvent 添加领域事件
func (a *Aggregate[T]) AddDomainEvent(evt eventing.IEvent) {
	if a.domainEvents == nil {
		a.domainEvents = make([]eventing.IEvent, 0)
	}
	a.domainEvents = append(a.domainEvents, evt)
}

// Validate 验证聚合根状态（默认实现）
func (a *Aggregate[T]) Validate() error {
	if a.IsDeleted() {
		return ErrAggregateDeleted
	}
	return nil
}
