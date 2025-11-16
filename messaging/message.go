// Package core 提供消息系统的核心抽象
package messaging

import (
	"time"
)

// 消息类型常量
const (
	MessageTypeEvent   = "event"
	MessageTypeCommand = "command"
	MessageTypeQuery   = "query"
)

// IMessage 消息接口
type IMessage interface {
	// GetID 获取消息ID
	GetID() string

	// GetType 获取消息类型
	GetType() string

	// GetTimestamp 获取时间戳
	GetTimestamp() time.Time

	// GetPayload 获取消息数据
	GetPayload() interface{}

	// GetMetadata 获取元数据
	GetMetadata() map[string]interface{}
}

// Message 消息基础实现
type Message struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   interface{}            `json:"payload"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	UserID    int64                  `json:"user_id,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
}

// GetID 获取消息ID
func (m *Message) GetID() string {
	return m.ID
}

// GetType 获取消息类型
func (m *Message) GetType() string {
	return m.Type
}

// GetTimestamp 获取时间戳
func (m *Message) GetTimestamp() time.Time {
	return m.Timestamp
}

// GetPayload 获取消息数据
func (m *Message) GetPayload() interface{} {
	return m.Payload
}

// GetMetadata 获取元数据
func (m *Message) GetMetadata() map[string]interface{} {
	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}
	return m.Metadata
}

// SetMetadata 设置元数据
func (m *Message) SetMetadata(key string, value interface{}) {
	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}
	m.Metadata[key] = value
}

// NewMessage 创建新消息
func NewMessage(messageID, messageType string, data interface{}) *Message {
	return &Message{
		ID:        messageID,
		Type:      messageType,
		Timestamp: time.Now(),
		Payload:   data,
		Metadata:  make(map[string]interface{}),
	}
}
