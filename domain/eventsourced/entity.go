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

	// ApplyEvent 确定性地应用事件到聚合根；不提供重复事件去重。
	//
	// 行为说明：
	//   - 聚合必须先通过 BindMetadata 绑定外层实例与预编译 handler metadata；
	//   - ApplyEvent 仅负责“重放/恢复”语义：调用 handler 更新业务状态，并在成功后递增版本号；
	//   - 相同初始状态与事件序列必须产生相同结果，handler 不得执行外部 I/O 或读取当前时间、随机数；
	//   - 若返回 error，则版本号保持不变，但业务字段可能已修改，调用方必须丢弃该实例。
	ApplyEvent(evt domain.IDomainEvent) error

	// GetUncommittedEvents 获取未提交的事件。
	GetUncommittedEvents() []domain.IDomainEvent

	// MarkEventsAsCommitted 标记事件为已提交。
	MarkEventsAsCommitted()

	// ClearUncommittedEvents 丢弃全部未提交事件（不改变版本号与 expectedVersion）。
	// 用于命令执行失败后清理部分应用的脏状态，避免失败聚合继续携带虚假的待提交事件。
	ClearUncommittedEvents()
}

// IReplayResettable 表示聚合能把事件溯源簿记重置到初始状态，用于快照加载失败
// 后降级为全量重放之前清理可能被部分反序列化污染的状态。
//
// 实现方必须保留 ID、聚合类型与已绑定的元数据（self/metadata），
// 仅把版本号、expectedVersion 与未提交事件缓冲区归零；业务字段的重置
// 由聚合自己的重建逻辑负责（全量重放会覆盖它们）。
type IReplayResettable interface {
	ResetForReplay()
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
// - self 必须非 nil，其类型与 aggregateType 必须匹配 metadata。
func (a *EventSourcedAggregate[T]) BindMetadata(self any, metadata *Metadata) error {
	if isNil(self) {
		return errors.NewCode(errors.InvalidInput, "aggregate self cannot be nil").
			WithContext("aggregate_type", a.aggregateType)
	}
	if metadata == nil || metadata.targetType == nil {
		return errors.NewCode(errors.InvalidInput, "aggregate metadata must be constructed by ScanMetadata or MetadataRegistry").
			WithContext("aggregate_type", a.aggregateType)
	}

	selfType := reflect.TypeOf(self)
	if selfType != metadata.targetType {
		return errors.NewCode(errors.Conflict, "aggregate metadata target type mismatch").
			WithContext("aggregate_type", a.aggregateType).
			WithContext("self_type", selfType.String()).
			WithContext("metadata_target_type", metadata.targetType.String())
	}
	if a.aggregateType != metadata.aggregateType {
		return errors.NewCode(errors.Conflict, "aggregate metadata type mismatch").
			WithContext("aggregate_type", a.aggregateType).
			WithContext("metadata_aggregate_type", metadata.aggregateType)
	}

	a.self = self
	a.metadata = metadata
	return nil
}

// RecordUncommittedEvent 将领域事件加入未提交事件列表。
func (a *EventSourcedAggregate[T]) RecordUncommittedEvent(evt domain.IDomainEvent) error {
	if isNil(evt) {
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

// ClearUncommittedEvents 丢弃全部未提交事件。
//
// 与 MarkEventsAsCommitted 的区别：仅清空待提交缓冲，不推进 expectedVersion，
// 也不改变版本号。命令执行/保存失败后调用，可防止部分应用的脏事件残留
// 在聚合实例上误导后续 Hook 或调用方。
func (a *EventSourcedAggregate[T]) ClearUncommittedEvents() {
	a.uncommittedEvents = nil
}

// ResetForReplay 把事件溯源簿记重置到初始状态，供快照加载失败后降级为
// 全量重放之前调用。保留 ID、聚合类型与已绑定元数据，仅清零版本与缓冲区。
func (a *EventSourcedAggregate[T]) ResetForReplay() {
	a.version = 0
	a.expectedVersion = 0
	a.uncommittedEvents = nil
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
//
// 注意：事件处理器返回错误时，处理器可能已经修改了聚合业务字段；调用方应丢弃该聚合实例并重新加载。
func (a *EventSourcedAggregate[T]) ApplyAndRecord(evt domain.IDomainEvent) error {
	return a.applyChange(evt, true)
}

// 编译期接口检查。
var _ IVersionSettable = (*EventSourcedAggregate[int64])(nil)

func (a *EventSourcedAggregate[T]) applyChange(evt domain.IDomainEvent, record bool) error {
	if isNil(evt) {
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

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
