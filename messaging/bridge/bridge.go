// Package bridge 提供远程桥接功能，支持跨服务的命令和事件通信
//
// 设计原则：
//   - 复用现有的 CommandBus 和 EventBus
//   - 最小化接口设计
//   - 支持多种传输协议
//   - 零侵入性集成
package bridge

import (
	"context"

	"gochen/messaging"
	"gochen/messaging/command"
)

// IRemoteBridge 远程桥接接口
//
// 定义跨服务通信的基本能力。
//
// 特性：
//   - 支持命令远程调用
//   - 支持事件远程发布
//   - 双向通信
//   - 可插拔实现
//
// 实现建议：
//   - HTTP/HTTPS
//   - gRPC
//   - 消息队列（RabbitMQ, Kafka）
//   - WebSocket
type IRemoteBridge interface {
	// SendCommand 发送命令到远程服务
	//
	// 参数：
	//   - ctx: 上下文
	//   - serviceURL: 远程服务地址
	//   - cmd: 命令实例
	//
	// 返回：
	//   - error: 发送失败错误
	//
	// 示例：
	//   err := bridge.SendCommand(ctx, "http://order-service:8080", cmd)
	SendCommand(ctx context.Context, serviceURL string, cmd *command.Command) error

	// SendEvent 发送事件消息到远程服务（基于 messaging 抽象）
	//
	// 参数：
	//   - ctx: 上下文
	//   - serviceURL: 远程服务地址
	//   - event: 事件消息（应满足 MessageTypeEvent 语义）
	//
	// 返回：
	//   - error: 发送失败错误
	//
	// 示例：
	//   err := bridge.SendEvent(ctx, "http://notification-service:8080", msg)
	SendEvent(ctx context.Context, serviceURL string, event messaging.IMessage) error

	// RegisterCommandHandler 注册命令处理器（服务端）
	//
	// 参数：
	//   - commandType: 命令类型
	//   - handler: 处理器函数
	//
	// 返回：
	//   - error: 注册失败错误
	//
	// 示例：
	//   bridge.RegisterCommandHandler("CreateOrder", myHandler)
	RegisterCommandHandler(commandType string, handler func(ctx context.Context, cmd *command.Command) error) error

	// RegisterEventHandler 注册事件处理器（服务端，基于 messaging 抽象）
	//
	// 参数：
	//   - eventType: 事件类型
	//   - handler: 处理器接口
	//
	// 返回：
	//   - error: 注册失败错误
	//
	// 示例：
	//   bridge.RegisterEventHandler("OrderCreated", myEventHandler)
	RegisterEventHandler(eventType string, handler messaging.IMessageHandler) error

	// Start 启动服务端（监听）
	//
	// 返回：
	//   - error: 启动失败错误
	Start() error

	// Stop 停止服务端
	//
	// 返回：
	//   - error: 停止失败错误
	Stop() error
}

// BridgeConfig 桥接配置
//
// 通用配置，不同实现可以扩展。
type BridgeConfig struct {
	// ServiceName 服务名称
	ServiceName string

	// ListenAddr 监听地址（服务端）
	ListenAddr string

	// EnableMetrics 启用指标收集
	EnableMetrics bool

	// EnableTracing 启用链路追踪
	EnableTracing bool
}

// DefaultBridgeConfig 默认配置
func DefaultBridgeConfig() *BridgeConfig {
	return &BridgeConfig{
		ServiceName:   "unknown",
		ListenAddr:    ":8080",
		EnableMetrics: false,
		EnableTracing: false,
	}
}
