package bridge

import (
	"encoding/json"
	"errors"

	"gochen/eventing"
	"gochen/messaging/command"
)

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

	// SerializeEvent 序列化事件
	SerializeEvent(event eventing.IEvent) ([]byte, error)

	// DeserializeEvent 反序列化事件
	DeserializeEvent(data []byte) (eventing.IEvent, error)
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
func (s *JSONSerializer) SerializeEvent(event eventing.IEvent) ([]byte, error) {
	if event == nil {
		return nil, ErrInvalidMessage
	}

	// 如果是 *eventing.Event 类型，直接序列化
	if e, ok := event.(*eventing.Event); ok {
		data, err := json.Marshal(e)
		if err != nil {
			return nil, errors.Join(ErrSerializationFailed, err)
		}
		return data, nil
	}

	// 否则，创建简化的事件结构
	simplified := map[string]interface{}{
		"id":             event.GetID(),
		"type":           event.GetType(),
		"aggregate_id":   event.GetAggregateID(),
		"aggregate_type": event.GetAggregateType(),
		"timestamp":      event.GetTimestamp(),
		"metadata":       event.GetMetadata(),
	}

	data, err := json.Marshal(simplified)
	if err != nil {
		return nil, errors.Join(ErrSerializationFailed, err)
	}

	return data, nil
}

// DeserializeEvent 反序列化事件
func (s *JSONSerializer) DeserializeEvent(data []byte) (eventing.IEvent, error) {
	if len(data) == 0 {
		return nil, ErrInvalidMessage
	}

	var event eventing.Event
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, errors.Join(ErrDeserializationFailed, err)
	}

	return &event, nil
}

// Ensure JSONSerializer implements ISerializer
var _ ISerializer = (*JSONSerializer)(nil)
