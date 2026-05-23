package eventsourced

import (
	"reflect"

	"gochen/domain"
	"gochen/errors"
)

// IEventSourcedAggregate 事件溯源聚合根接口。
// 使用事件溯源模式的聚合根，状态完全由事件重建。
type IEventSourcedAggregate[T comparable] interface {
	domain.IEntity[T]

	// GetAggregateType 返回聚合根类型名称。
	GetAggregateType() string

	// GetExpectedVersion 返回当前待提交事件批次对应的基线版本。
	//
	// 说明：
	//   - 对新建或已提交完成的聚合，该值等于当前 GetVersion()；
	//   - 对存在未提交事件的聚合，该值等于“本批未提交事件应用前”的版本号；
	//   - 仓储保存时必须使用该显式基线版本，而不是再通过 version - len(events) 反推。
	GetExpectedVersion() uint64

	// ApplyEvent 应用事件到聚合根（修改状态），应为幂等。
	//
	// 行为说明：
	//   - 聚合必须先通过 BindMetadata 绑定外层实例与预编译 handler metadata；
	//   - ApplyEvent 仅负责“重放/恢复”语义：调用 handler 更新业务状态，并在成功后递增版本号；
	//   - 若返回 error，则版本号保持不变。
	ApplyEvent(evt domain.IDomainEvent) error

	// GetUncommittedEvents 获取未提交的事件。
	GetUncommittedEvents() []domain.IDomainEvent

	// MarkEventsAsCommitted 标记事件为已提交。
	MarkEventsAsCommitted()
}

// IVersionSettable 可设置版本的聚合接口（可选）。
//
// 用于快照恢复时显式设置聚合版本号。
// 聚合可选择实现此接口以支持快照恢复后正确设置版本。
type IVersionSettable interface {
	// SetVersion 设置聚合版本号。
	// 仅供快照恢复使用，业务代码不应直接调用。
	SetVersion(version uint64)
}

// EventSourcedAggregate 事件溯源聚合根（泛型实现）。
//
// 推荐用法：在嵌入字段上声明 `aggregate:"xxx"` tag，并使用 eventsourced.New 进行构造。
//
//	type Account struct {
//	    *eventsourced.EventSourcedAggregate[int64] `aggregate:"account"`
//	    Balance int
//	}
//
//	func NewAccount(registry *eventsourced.MetadataRegistry, id int64) (*Account, error) {
//	    return eventsourced.New[Account, int64](registry, id)
//	}
//
//	func (a *Account) ApplyDeposited(e *Deposited) {
//	    a.Balance += e.Amount
//	}
//
//	func (a *Account) Deposit(amount int) error {
//	    return a.ApplyAndRecord(&Deposited{Amount: amount})
//	}
type EventSourcedAggregate[T comparable] struct {
	id                T
	version           uint64
	expectedVersion   uint64
	uncommittedEvents []domain.IDomainEvent
	aggregateType     string

	self     any
	metadata *Metadata
}

// NewEventSourcedAggregate 创建事件溯源聚合根，并初始化未提交事件缓冲区。
func NewEventSourcedAggregate[T comparable](id T, aggregateType string) *EventSourcedAggregate[T] {
	return &EventSourcedAggregate[T]{
		id:                id,
		version:           0,
		expectedVersion:   0,
		uncommittedEvents: make([]domain.IDomainEvent, 0),
		aggregateType:     aggregateType,
	}
}

// GetID 返回聚合根 ID。
func (a *EventSourcedAggregate[T]) GetID() T {
	return a.id
}

func (a *EventSourcedAggregate[T]) GetVersion() uint64 {
	return a.version
}

func (a *EventSourcedAggregate[T]) GetExpectedVersion() uint64 {
	return a.expectedVersion
}

// SetVersion 设置版本。
func (a *EventSourcedAggregate[T]) SetVersion(version uint64) {
	a.version = version
	a.expectedVersion = version
}

func (a *EventSourcedAggregate[T]) GetAggregateType() string {
	return a.aggregateType
}

// BindMetadata 显式绑定外层聚合实例与预编译元数据。
//
// 说明：
// - 推荐在聚合构造函数或仓储装配阶段调用；
// - 该路径不会触发新的反射扫描，运行时仅消费预编译元数据；
// - self 与 metadata 的目标类型必须一致，aggregateType 也必须一致（若双方都非空）。
func (a *EventSourcedAggregate[T]) BindMetadata(self any, metadata *Metadata) error {
	if self == nil {
		return errors.NewCode(errors.InvalidInput, "aggregate self cannot be nil").
			WithContext("aggregate_type", a.aggregateType)
	}
	if metadata == nil {
		return errors.NewCode(errors.InvalidInput, "aggregate metadata cannot be nil").
			WithContext("aggregate_type", a.aggregateType)
	}

	selfType := reflect.TypeOf(self)
	if metadata.TargetType != nil && selfType != metadata.TargetType {
		return errors.NewCode(errors.Conflict, "aggregate metadata target type mismatch").
			WithContext("aggregate_type", a.aggregateType).
			WithContext("self_type", selfType.String()).
			WithContext("metadata_target_type", metadata.TargetType.String())
	}
	if a.aggregateType != "" && metadata.AggregateType != "" && a.aggregateType != metadata.AggregateType {
		return errors.NewCode(errors.Conflict, "aggregate metadata type mismatch").
			WithContext("aggregate_type", a.aggregateType).
			WithContext("metadata_aggregate_type", metadata.AggregateType)
	}

	a.self = self
	a.metadata = metadata
	return nil
}

// RecordUncommittedEvent 将领域事件加入未提交事件列表。
func (a *EventSourcedAggregate[T]) RecordUncommittedEvent(evt domain.IDomainEvent) error {
	if evt == nil {
		return errors.NewCode(errors.InvalidInput, "event cannot be nil").
			WithContext("aggregate_type", a.aggregateType).
			WithContext("aggregate_id", a.id)
	}
	a.uncommittedEvents = append(a.uncommittedEvents, evt)
	return nil
}

// GetUncommittedEvents 返回未提交事件的副本切片，避免调用方直接修改内部缓冲区。
func (a *EventSourcedAggregate[T]) GetUncommittedEvents() []domain.IDomainEvent {
	if len(a.uncommittedEvents) == 0 {
		return nil
	}
	events := make([]domain.IDomainEvent, len(a.uncommittedEvents))
	copy(events, a.uncommittedEvents)
	return events
}

// MarkEventsAsCommitted 标记事件为已提交。
func (a *EventSourcedAggregate[T]) MarkEventsAsCommitted() {
	a.uncommittedEvents = nil
	a.expectedVersion = a.version
}

// ApplyEvent 应用事件到聚合根。
func (a *EventSourcedAggregate[T]) ApplyEvent(evt domain.IDomainEvent) error {
	return a.applyChange(evt, false)
}

// Validate 校验输入。
func (a *EventSourcedAggregate[T]) Validate() error {
	return nil
}

// ApplyAndRecord 应用事件并记录为未提交。
func (a *EventSourcedAggregate[T]) ApplyAndRecord(evt domain.IDomainEvent) error {
	return a.applyChange(evt, true)
}

// 编译期接口检查。
var _ IVersionSettable = (*EventSourcedAggregate[int64])(nil)

func (a *EventSourcedAggregate[T]) applyChange(evt domain.IDomainEvent, record bool) error {
	if evt == nil {
		return errors.NewCode(errors.InvalidInput, "event cannot be nil").
			WithContext("aggregate_type", a.aggregateType).
			WithContext("aggregate_id", a.id)
	}
	if a.self == nil || a.metadata == nil {
		return errors.NewCode(errors.Internal, "aggregate metadata is not bound").
			WithContext("aggregate_type", a.aggregateType).
			WithContext("aggregate_id", a.id)
	}

	handled, err := ApplyWithMetadata(a.self, a.metadata, evt)
	if err != nil {
		return err
	}
	if !handled {
		return errors.NewCode(errors.Internal, "event handler not found").
			WithContext("aggregate_type", a.aggregateType).
			WithContext("aggregate_id", a.id).
			WithContext("event_type", evt.EventType()).
			WithContext("event_go_type", reflect.TypeOf(evt).String())
	}

	baseVersion := a.version
	a.version++
	if record {
		if len(a.uncommittedEvents) == 0 {
			a.expectedVersion = baseVersion
		}
		a.uncommittedEvents = append(a.uncommittedEvents, evt)
	}
	return nil
}
