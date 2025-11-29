package bridge

import (
	"encoding/json"
	"errors"

	"gochen/messaging"
	"gochen/messaging/command"
)

// messageWithRaw 封装消息体并保留反序列化的原始字段，便于上层做二次映射（如还原 eventing.Event 的聚合字段）。
type messageWithRaw struct {
	messaging.Message
	raw map[string]any `json:"-"`
}

// RawData 返回原始字段映射
func (m *messageWithRaw) RawData() map[string]any {
	return m.raw
}

// ISerializer 序列化器接口
//
// 定义消息序列化/反序列化的能力。
//
// 实现：
//   - JSON（默认）
//   - Protobuf
//   - MessagePack
type ISerializer interface {
	// SerializeCommand 序列化命令
	SerializeCommand(cmd *command.Command) ([]byte, error)

	// DeserializeCommand 反序列化命令
	DeserializeCommand(data []byte) (*command.Command, error)

	// SerializeEvent 序列化事件消息（基于 messaging 抽象）
	SerializeEvent(event messaging.IMessage) ([]byte, error)

	// DeserializeEvent 反序列化事件消息
	DeserializeEvent(data []byte) (messaging.IMessage, error)
}

// JSONSerializer JSON 序列化器
//
// 默认使用 JSON 序列化，简单且兼容性好。
type JSONSerializer struct{}

// NewJSONSerializer 创建 JSON 序列化器
func NewJSONSerializer() *JSONSerializer {
	return &JSONSerializer{}
}

// SerializeCommand 序列化命令
func (s *JSONSerializer) SerializeCommand(cmd *command.Command) ([]byte, error) {
	if cmd == nil {
		return nil, ErrInvalidMessage
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, errors.Join(ErrSerializationFailed, err)
	}

	return data, nil
}

// DeserializeCommand 反序列化命令
func (s *JSONSerializer) DeserializeCommand(data []byte) (*command.Command, error) {
	if len(data) == 0 {
		return nil, ErrInvalidMessage
	}

	var cmd command.Command
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, errors.Join(ErrDeserializationFailed, err)
	}

	return &cmd, nil
}

// SerializeEvent 序列化事件
func (s *JSONSerializer) SerializeEvent(event messaging.IMessage) ([]byte, error) {
	if event == nil {
		return nil, ErrInvalidMessage
	}

	// 尝试直接序列化，如果失败再回退到基础字段抽取
	data, err := json.Marshal(event)
	if err != nil {
		// 回退：抽取基础字段，避免非导出字段导致序列化失败
		simplified := map[string]any{
			"id":        event.GetID(),
			"type":      event.GetType(),
			"timestamp": event.GetTimestamp(),
			"metadata":  event.GetMetadata(),
			"payload":   event.GetPayload(),
		}

		data, err = json.Marshal(simplified)
		if err != nil {
			return nil, errors.Join(ErrSerializationFailed, err)
		}
	}

	return data, nil
}

// DeserializeEvent 反序列化事件
func (s *JSONSerializer) DeserializeEvent(data []byte) (messaging.IMessage, error) {
	if len(data) == 0 {
		return nil, ErrInvalidMessage
	}

	raw := make(map[string]any)
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.Join(ErrDeserializationFailed, err)
	}

	var event messaging.Message
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, errors.Join(ErrDeserializationFailed, err)
	}

	if event.Metadata == nil {
		event.Metadata = make(map[string]any)
	}

	return &messageWithRaw{
		Message: event,
		raw:     raw,
	}, nil
}

// Ensure JSONSerializer implements ISerializer
var _ ISerializer = (*JSONSerializer)(nil)
