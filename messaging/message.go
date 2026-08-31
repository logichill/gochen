package messaging

import (
	"reflect"
	"time"

	"gochen/clock"
)

var defaultMessageClock = clock.NewRealClock()

// MessageKind 表示消息类别（用于语义判定/观测，不承担路由职责）。
type MessageKind string

const (
	// KindUnknown 未知类型（缺省值）。
	KindUnknown MessageKind = "unknown"
	// KindCommand 命令消息。
	KindCommand MessageKind = "command"
	// KindEvent 事件消息。
	KindEvent MessageKind = "event"
	// KindQuery 查询消息。
	KindQuery MessageKind = "query"
)

// IMessage 消息接口（信封层最小公共视图）。
//
// 并发约定：消息对象交给 transport 发布后，调用方不得再修改该对象或其返回的 Payload/Metadata；
// transport 会在接收时复制信封以隔离后续处理，但不会与调用方的并发写入同步。
type IMessage interface {
	// GetID 获取消息ID。
	GetID() string
	// GetKind 获取消息类别（command/event/query 等）。
	GetKind() MessageKind
	// GetType 获取消息类型（具体消息名，用于路由与订阅键）。
	GetType() string
	// GetTimestamp 获取时间戳。
	GetTimestamp() time.Time
	// GetPayload 获取消息数据（载荷）。
	GetPayload() Payload
	// GetMetadata 获取元数据。
	GetMetadata() *Metadata
}

// Message 消息基础实现。
type Message struct {
	ID        string      `json:"id"`
	Kind      MessageKind `json:"kind,omitempty"`
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   Payload     `json:"payload"`
	Metadata  *Metadata   `json:"metadata,omitempty"`
}

// GetID 返回消息 ID。
func (m *Message) GetID() string { return m.ID }

func (m *Message) GetKind() MessageKind {
	if m.Kind == "" {
		return KindUnknown
	}
	return m.Kind
}

// GetType 返回消息类型（用于路由与订阅键）。
func (m *Message) GetType() string { return m.Type }

// GetTimestamp 返回消息时间戳。
func (m *Message) GetTimestamp() time.Time { return m.Timestamp }

func (m *Message) GetPayload() Payload { return m.Payload }

func (m *Message) GetMetadata() *Metadata {
	if m.Metadata == nil {
		m.Metadata = NewMetadata()
	}
	return m.Metadata
}

// SetMetadata 设置元数据（便捷方法）。
func (m *Message) SetMetadata(key, value string) {
	m.GetMetadata().Set(key, value)
}

// NewMessage 创建一条消息（时间来源使用默认 clock）。
func NewMessage(messageID string, kind MessageKind, messageType string, data any) *Message {
	return NewMessageWithClock(defaultMessageClock, messageID, kind, messageType, data)
}

// NewMessageWithClock 创建消息并注入时间来源。
func NewMessageWithClock(clk clock.IClock, messageID string, kind MessageKind, messageType string, data any) *Message {
	if isNilMessageClock(clk) {
		clk = defaultMessageClock
	}
	return &Message{
		ID:        messageID,
		Kind:      kind,
		Type:      messageType,
		Timestamp: clk.Now(),
		Payload:   NewPayload(data),
		Metadata:  NewMetadata(),
	}
}

func isNilMessageClock(clk clock.IClock) bool {
	if clk == nil {
		return true
	}
	rv := reflect.ValueOf(clk)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
