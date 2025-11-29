package bridge

import "errors"

// 桥接相关错误
var (
	// ErrInvalidMessage 无效的消息
	ErrInvalidMessage = errors.New("invalid message")

	// ErrSerializationFailed 序列化失败
	ErrSerializationFailed = errors.New("serialization failed")

	// ErrDeserializationFailed 反序列化失败
	ErrDeserializationFailed = errors.New("deserialization failed")

	// ErrRemoteCallFailed 远程调用失败
	ErrRemoteCallFailed = errors.New("remote call failed")

	// ErrTimeout 超时
	ErrTimeout = errors.New("timeout")

	// ErrServiceUnavailable 服务不可用
	ErrServiceUnavailable = errors.New("service unavailable")

	// ErrHandlerNotFound 处理器未找到
	ErrHandlerNotFound = errors.New("handler not found")

	// ErrBridgeNotStarted 桥接未启动
	ErrBridgeNotStarted = errors.New("bridge not started")
)
