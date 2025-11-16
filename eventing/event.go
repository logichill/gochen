package eventing

import (
	"fmt"
	"strconv"
	"time"

	"gochen/idgen/snowflake"
	"gochen/messaging"
)

// IEvent 基础事件接口（用于事件传输/路由）
// 包含事件分发的最小必要信息
type IEvent interface {
	messaging.IMessage

	// 聚合信息（用于路由和关联）
	GetAggregateID() int64
	GetAggregateType() string
	GetVersion() uint64
}

// IStorableEvent 扩展事件接口（用于事件持久化）
// 包含存储相关的所有方法
type IStorableEvent interface {
	IEvent // 继承基础接口

	// 存储相关方法
	GetSchemaVersion() int
	SetAggregateType(string)
	Validate() error
}

// Event 领域事件实现
// 同时实现了 IEvent 和 IStorableEvent 接口
type Event struct {
	messaging.Message
	AggregateID   int64  `json:"aggregate_id"`
	AggregateType string `json:"aggregate_type"`
	Version       uint64 `json:"version"`
	SchemaVersion int    `json:"schema_version"`
}

// 基础接口实现
func (e *Event) GetAggregateID() int64    { return e.AggregateID }
func (e *Event) GetAggregateType() string { return e.AggregateType }
func (e *Event) GetVersion() uint64       { return e.Version }

// 扩展接口实现
func (e *Event) GetSchemaVersion() int {
	if e.SchemaVersion <= 0 {
		return 1
	}
	return e.SchemaVersion
}

func (e *Event) SetAggregateType(t string) { e.AggregateType = t }

func (e *Event) Validate() error {
	if e.GetID() == "" {
		return fmt.Errorf("事件ID不能为空")
	}
	if e.AggregateID <= 0 {
		return fmt.Errorf("聚合ID必须大于0")
	}
	if e.AggregateType == "" {
		return fmt.Errorf("聚合类型不能为空")
	}
	if e.GetType() == "" {
		return fmt.Errorf("事件类型不能为空")
	}
	if e.Version <= 0 {
		return fmt.Errorf("事件版本必须大于0")
	}
	if e.SchemaVersion <= 0 {
		return fmt.Errorf("事件模式版本必须大于0")
	}
	return nil
}

// NewEvent 创建事件
func NewEvent(aggregateID int64, aggregateType, eventType string, version uint64, data interface{}, schemaVersion ...int) *Event {
	id := snowflake.Generate()
	sVersion := 1
	if len(schemaVersion) > 0 && schemaVersion[0] > 0 {
		sVersion = schemaVersion[0]
	}
	return &Event{
		Message: messaging.Message{
			ID:        strconv.FormatInt(id, 10),
			Type:      eventType,
			Timestamp: time.Now(),
			Payload:   data,
			Metadata:  make(map[string]interface{}),
		},
		AggregateID:   aggregateID,
		AggregateType: aggregateType,
		Version:       version,
		SchemaVersion: sVersion,
	}
}

// NewDomainEvent 语义化别名
func NewDomainEvent(aggregateID int64, aggregateType, eventType string, version uint64, data interface{}, schemaVersion ...int) *Event {
	e := NewEvent(aggregateID, aggregateType, eventType, version, data, schemaVersion...)
	metadata := e.GetMetadata()
	metadata["source"] = "domain"
	metadata["event_sourced"] = true
	return e
}
