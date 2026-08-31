package messaging

import (
	"fmt"

	"gochen/errors"
	"gochen/internal/clonevalue"
)

// IMessageEnvelopeCloner 是自定义消息信封的显式克隆契约。
//
// 自定义 IMessage 若会进入异步传输层，必须实现该接口；否则传输层会拒绝发布，
// 避免把未知具体类型降级成基础快照后丢失业务方法或强类型字段。
type IMessageEnvelopeCloner interface {
	// CloneMessageEnvelope 返回保留具体类型的消息信封副本。
	CloneMessageEnvelope() IMessage
}

// IImmutableMessageEnvelope 标记自定义消息信封可安全复用同一实例。
//
// 仅当消息实现保证发布后信封、payload 与 metadata 不再发生可见变更时才应实现该接口；
// 异步传输会直接保留原实例，不再尝试深拷贝。
type IImmutableMessageEnvelope interface {
	// ImmutableMessageEnvelope 标记消息信封可跨异步边界安全复用。
	ImmutableMessageEnvelope()
}

// CloneValue 深拷贝框架消息载荷中常见的可变值形态。
func CloneValue(value any) any {
	return clonevalue.Clone(value)
}

// CloneMessageEnvelope 按 messaging 明确契约克隆消息信封。
//
// 支持的形态：
// - *Message：深拷贝基础信封；
// - IMessageEnvelopeCloner：由消息类型自行保留具体类型并克隆；
// - IImmutableMessageEnvelope：显式声明不可变时复用原实例。
//
// 其他自定义 IMessage 会返回 InvalidInput，避免静默降级丢失 concrete type。
func CloneMessageEnvelope(message IMessage) (IMessage, error) {
	if message == nil {
		return nil, errors.NewCode(errors.InvalidInput, "message is nil")
	}
	if cloneable, ok := message.(IMessageEnvelopeCloner); ok {
		cloned := cloneable.CloneMessageEnvelope()
		if cloned == nil {
			return nil, errors.NewCode(errors.InvalidInput, "message clone returned nil").
				WithContext("message_type", message.GetType()).
				WithContext("concrete_type", fmt.Sprintf("%T", message))
		}
		return cloned, nil
	}
	if base, ok := message.(*Message); ok {
		clone := CloneMessage(*base)
		return &clone, nil
	}
	if _, ok := message.(IImmutableMessageEnvelope); ok {
		return message, nil
	}
	return nil, errors.NewCode(errors.InvalidInput, "custom message must implement messaging.IMessageEnvelopeCloner or messaging.IImmutableMessageEnvelope").
		WithContext("message_type", message.GetType()).
		WithContext("concrete_type", fmt.Sprintf("%T", message))
}

// CloneMessage 深拷贝基础消息信封，避免异步传输复用调用方的可变 payload/metadata。
func CloneMessage(message Message) Message {
	clone := message
	clone.Payload = NewPayload(CloneValue(PayloadValue(message.GetPayload())))
	clone.Metadata = CloneMetadata(message.Metadata)
	return clone
}

// CloneMetadata 深拷贝消息 metadata。
func CloneMetadata(metadata *Metadata) *Metadata {
	if metadata == nil {
		return nil
	}
	clone := NewMetadata()
	for k, v := range metadata.MapCopy() {
		clone.Set(k, v)
	}
	return clone
}
