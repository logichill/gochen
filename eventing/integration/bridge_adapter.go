package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/messaging"
	"gochen/messaging/bridge"
)

// RegisterBridgeEventHandler 通过 bridge.IRemoteBridge 注册事件处理器，自动将 messaging.IMessage 转换为 eventing.IEvent。
func RegisterBridgeEventHandler(br bridge.IRemoteBridge, eventType string, handler bus.IEventHandler) error {
	if br == nil {
		return fmt.Errorf("bridge is nil")
	}
	if handler == nil {
		return fmt.Errorf("event handler is nil")
	}
	return br.RegisterEventHandler(eventType, &eventHandlerAdapter{handler: handler})
}

// eventHandlerAdapter 将 IMessage 转换为 IEvent 后再委托给原始 IEventHandler。
type eventHandlerAdapter struct {
	handler bus.IEventHandler
}

func (a *eventHandlerAdapter) Handle(ctx context.Context, msg messaging.IMessage) error {
	evt, err := ToEvent(msg)
	if err != nil {
		return err
	}
	return a.handler.HandleEvent(ctx, evt)
}

func (a *eventHandlerAdapter) Type() string { return a.handler.Type() }

// rawCarrier 用于从反序列化后的消息中获取原始字段，避免聚合信息丢失。
type rawCarrier interface {
	RawData() map[string]any
}

// ToEvent 将任意 messaging.IMessage 转换为 eventing.IEvent。
// 优先返回已实现 IEvent 的消息；否则从 RawData/Metadata 恢复聚合字段。
func ToEvent(msg messaging.IMessage) (eventing.IEvent, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
	}

	// 已是 IEvent 直接返回
	if evt, ok := msg.(eventing.IEvent); ok {
		return evt, nil
	}

	raw := map[string]any{}
	if carrier, ok := msg.(rawCarrier); ok && carrier.RawData() != nil {
		raw = carrier.RawData()
	}
	meta := msg.GetMetadata()

	aggID := pickInt64(raw, meta, "aggregate_id")
	aggType := pickString(raw, meta, "aggregate_type")
	version := pickUint64(raw, meta, "version")
	schema := pickInt(raw, meta, "schema_version")
	if schema <= 0 {
		schema = 1
	}
	if version == 0 {
		version = 1
	}

	return &eventing.Event{
		Message: messaging.Message{
			ID:        msg.GetID(),
			Type:      msg.GetType(),
			Timestamp: msg.GetTimestamp(),
			Payload:   msg.GetPayload(),
			Metadata:  meta,
		},
		AggregateID:   aggID,
		AggregateType: aggType,
		Version:       version,
		SchemaVersion: schema,
	}, nil
}

func pickString(raw, meta map[string]any, key string) string {
	if v, ok := raw[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if v, ok := meta[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func pickInt64(raw, meta map[string]any, key string) int64 {
	if v, ok := raw[key]; ok {
		if n, ok := toInt64(v); ok {
			return n
		}
	}
	if v, ok := meta[key]; ok {
		if n, ok := toInt64(v); ok {
			return n
		}
	}
	return 0
}

func pickUint64(raw, meta map[string]any, key string) uint64 {
	if v, ok := raw[key]; ok {
		if n, ok := toUint64(v); ok {
			return n
		}
	}
	if v, ok := meta[key]; ok {
		if n, ok := toUint64(v); ok {
			return n
		}
	}
	return 0
}

func pickInt(raw, meta map[string]any, key string) int {
	if v, ok := raw[key]; ok {
		if n, ok := toInt(v); ok {
			return n
		}
	}
	if v, ok := meta[key]; ok {
		if n, ok := toInt(v); ok {
			return n
		}
	}
	return 0
}

func toInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case int32:
		return int64(t), true
	case float64:
		return int64(t), true
	case json.Number:
		n, err := strconv.ParseInt(string(t), 10, 64)
		if err == nil {
			return n, true
		}
	case string:
		n, err := strconv.ParseInt(t, 10, 64)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

func toUint64(v any) (uint64, bool) {
	switch t := v.(type) {
	case uint64:
		return t, true
	case uint:
		return uint64(t), true
	case uint32:
		return uint64(t), true
	case int:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case int64:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case float64:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case json.Number:
		n, err := strconv.ParseUint(string(t), 10, 64)
		if err == nil {
			return n, true
		}
	case string:
		n, err := strconv.ParseUint(t, 10, 64)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

func toInt(v any) (int, bool) {
	if n, ok := toInt64(v); ok {
		return int(n), true
	}
	return 0, false
}
