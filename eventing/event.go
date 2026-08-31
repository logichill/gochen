package eventing

import (
	"gochen/errors"
	"gochen/gen"
	"gochen/messaging"
)

// IEvent 基础事件接口（用于事件传输/路由）
// 只关心事件在消息通道中的通用视图（类型、时间戳等），不绑定具体的聚合 ID 类型。
//
// 注意：Event.Version 与 Entity.Version 语义不同：
//   - Event.Version: 事件在聚合事件流中的序号（uint64），用于事件排序、重放和乐观并发控制。
//   - Entity.Version: 实体的乐观锁版本号（通常为 uint64/int64），用于检测 CRUD 操作的并发修改冲突。
type IEvent interface {
	messaging.IMessage

	// 聚合类型（用于路由和关联）
	GetAggregateType() string
	// GetVersion 返回事件在聚合事件流中的序号
	// 从 1 开始递增，用于保证事件顺序和实现乐观并发控制
	GetVersion() uint64
}

// ITypedEvent 带强类型聚合 ID 的事件接口。
//
// ID 为聚合根 ID 类型（如 int64、string、自定义类型等）。
type ITypedEvent[ID comparable] interface {
	IEvent
	GetAggregateID() ID
}

// IStorableEvent 扩展事件接口（用于事件持久化），带强类型聚合 ID。
//
// 包含存储相关的所有方法。
type IStorableEvent[ID comparable] interface {
	ITypedEvent[ID]

	// 存储相关方法
	EventSchemaVersion() int
	SetAggregateType(string)
	Validate() error
}

// Event 泛型事件实现。
//
// ID 为聚合根 ID 类型（如 int64、string、自定义类型等），
// 同一进程内可以根据需要实例化为不同的 ID 形态：
//   - Event[int64]  ：默认的数值型聚合 ID
//   - Event[string] ：字符串/UUID 聚合 ID
//   - Event[MyID]   ：自定义强类型聚合 ID
type Event[ID comparable] struct {
	messaging.Message
	AggregateID    ID     `json:"aggregate_id"`
	AggregateType  string `json:"aggregate_type"`
	Version        uint64 `json:"version"`
	SchemaVersion  int    `json:"schema_version"`
	GlobalPosition int64  `json:"global_position,omitempty"`
}

// GetAggregateID 返回事件所属聚合的强类型 ID。
func (e *Event[ID]) GetAggregateID() ID { return e.AggregateID }

func (e *Event[ID]) GetAggregateType() string { return e.AggregateType }

func (e *Event[ID]) GetVersion() uint64 { return e.Version }

// GetGlobalPosition 返回事件在全局事件流中的单调位置；0 表示未知、未由存储分配或 nil 事件。
func (e *Event[ID]) GetGlobalPosition() int64 {
	if e == nil {
		return 0
	}
	return e.GlobalPosition
}

// CloneMessageEnvelope 返回事件信封副本，供异步传输在入队前冻结可变字段。
func (e *Event[ID]) CloneMessageEnvelope() messaging.IMessage {
	if e == nil {
		return nil
	}
	clone := &Event[ID]{
		Message:        messaging.CloneMessage(e.Message),
		AggregateID:    e.AggregateID,
		AggregateType:  e.AggregateType,
		Version:        e.Version,
		SchemaVersion:  e.SchemaVersion,
		GlobalPosition: e.GlobalPosition,
	}
	return clone
}

// 扩展接口实现（IStorableEvent）

// SchemaVersion 返回事件载荷的 schema 版本；未显式设置时默认为 1。
func (e *Event[ID]) EventSchemaVersion() int {
	if e.SchemaVersion <= 0 {
		return 1
	}
	return e.SchemaVersion
}

// SetAggregateType 设置事件所属聚合类型。
func (e *Event[ID]) SetAggregateType(t string) { e.AggregateType = t }

// Validate 校验输入是否满足约束。
func (e *Event[ID]) Validate() error {
	if e.GetID() == "" {
		return errors.NewCode(errors.Validation, "event ID cannot be empty")
	}
	if e.AggregateType == "" {
		return errors.NewCode(errors.Validation, "aggregate type cannot be empty")
	}
	if e.GetType() == "" {
		return errors.NewCode(errors.Validation, "event type cannot be empty")
	}
	if e.Version <= 0 {
		return errors.NewCode(errors.Validation, "event version must be greater than 0")
	}
	if e.SchemaVersion < 0 {
		return errors.NewCode(errors.Validation, "schema version must be non-negative")
	}

	// 对数值类型的聚合 ID 做基础校验（>0），
	// 对其他类型（string/自定义类型）不做强制约束。
	switch v := any(e.AggregateID).(type) {
	case int:
		if v <= 0 {
			return errors.NewCode(errors.Validation, "aggregate ID must be greater than 0")
		}
	case int64:
		if v <= 0 {
			return errors.NewCode(errors.Validation, "aggregate ID must be greater than 0")
		}
	case uint:
		if v == 0 {
			return errors.NewCode(errors.Validation, "aggregate ID must be greater than 0")
		}
	case uint64:
		if v == 0 {
			return errors.NewCode(errors.Validation, "aggregate ID must be greater than 0")
		}
	}

	return nil
}

func nextEventID(g gen.IGenerator[string]) (string, error) {
	if g == nil {
		return "", errors.NewCode(errors.InvalidInput, "event ID generator cannot be nil")
	}
	id, err := g.Next()
	if err != nil {
		return "", errors.NewCodeWithCause(errors.Dependency, "generate event ID failed", err)
	}
	if id == "" {
		return "", errors.NewCode(errors.Internal, "event ID generator returned empty ID")
	}
	return id, nil
}

// NewEvent 创建事件，并显式使用 generator 生成事件 ID。
func NewEvent[ID comparable](g gen.IGenerator[string], aggregateID ID, aggregateType, eventType string, version uint64, data any, schemaVersion ...int) (*Event[ID], error) {
	id, err := nextEventID(g)
	if err != nil {
		return nil, err
	}
	return NewEventWithID(id, aggregateID, aggregateType, eventType, version, data, schemaVersion...), nil
}

// NewEventWithID 使用调用方已分配的事件 ID 创建事件。
func NewEventWithID[ID comparable](eventID string, aggregateID ID, aggregateType, eventType string, version uint64, data any, schemaVersion ...int) *Event[ID] {
	sVersion := 1
	if len(schemaVersion) > 0 && schemaVersion[0] > 0 {
		sVersion = schemaVersion[0]
	}
	msg := messaging.NewMessage(eventID, messaging.KindEvent, eventType, data)
	return &Event[ID]{
		Message:       *msg,
		AggregateID:   aggregateID,
		AggregateType: aggregateType,
		Version:       version,
		SchemaVersion: sVersion,
	}
}

// 编译期断言：确保默认形态 Event[int64] 满足接口约束。
var (
	_ IEvent                = (*Event[int64])(nil)
	_ ITypedEvent[int64]    = (*Event[int64])(nil)
	_ IStorableEvent[int64] = (*Event[int64])(nil)
)
