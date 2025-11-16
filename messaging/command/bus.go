package command

import (
	"context"
	"fmt"
	"sync"

	"gochen/messaging"
)

// CommandBus 命令总线
//
// CommandBus 是对 MessageBus 的包装，提供命令特定的语义和便利方法。
// 它复用 MessageBus 的所有能力（中间件、传输层等），只添加命令特定的扩展。
//
// 特性：
//   - 基于 MessageBus，不重复实现
//   - 提供命令特定的 API（Dispatch, RegisterHandler 等）
//   - 可选的聚合级并发控制
//   - 完全兼容现有的中间件和传输层
type CommandBus struct {
	messageBus messaging.IMessageBus // 底层消息总线

	// handlers 命令处理器注册表
	// key: commandType, value: handler
	handlers map[string]messaging.IMessageHandler
	mutex    sync.RWMutex
}

// commandRoutingHandler 根据命令类型进行路由的适配器
//
// 它订阅在统一的消息类型（messaging.MessageTypeCommand）上，
// 根据 Command.Metadata["command_type"] 与 commandType 的匹配结果决定是否处理。
type commandRoutingHandler struct {
	commandType string
	inner       messaging.IMessageHandler
}

func (h *commandRoutingHandler) Handle(ctx context.Context, message messaging.IMessage) error {
	cmd, ok := message.(*Command)
	if !ok {
		// 非 Command，忽略
		return nil
	}
	if cmd.GetCommandType() != h.commandType {
		// 其他命令类型，忽略
		return nil
	}
	return h.inner.Handle(ctx, message)
}

func (h *commandRoutingHandler) Type() string {
	return h.inner.Type()
}

// CommandBusConfig 命令总线配置
type CommandBusConfig struct {
	// EnableAggregateLock 是否启用聚合级锁
	// true: 同一聚合的命令串行执行（避免并发冲突）
	// false: 允许并发执行（需要应用层处理并发）
	EnableAggregateLock bool
}

// DefaultCommandBusConfig 默认配置
func DefaultCommandBusConfig() *CommandBusConfig {
	return &CommandBusConfig{
		EnableAggregateLock: false, // 默认不启用，避免性能影响
	}
}

// NewCommandBus 创建命令总线
//
// 参数：
//   - messageBus: 底层消息总线（复用）
//   - config: 配置，当前仅为兼容保留，聚合锁应通过中间件实现
//
// 返回：
//   - *CommandBus: 命令总线实例
func NewCommandBus(messageBus messaging.IMessageBus, config *CommandBusConfig) *CommandBus {
	if config == nil {
		config = DefaultCommandBusConfig()
	}

	return &CommandBus{
		messageBus: messageBus,
		handlers:   make(map[string]messaging.IMessageHandler),
	}
}

// RegisterHandler 注册命令处理器
//
// 参数：
//   - commandType: 命令类型（例如："CreateUser"）
//   - handler: 命令处理函数
//
// 返回：
//   - error: 注册失败时返回错误
func (bus *CommandBus) RegisterHandler(commandType string, handler CommandHandlerFunc) error {
	if commandType == "" {
		return fmt.Errorf("command type cannot be empty")
	}

	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	// 基础命令处理器（直接处理 *Command 并回传结果）
	baseHandler := handler.AsMessageHandler(commandType)

	// 路由包装器：只处理指定 commandType 的命令，
	// 订阅在统一的命令消息类型上（messaging.MessageTypeCommand）
	routingHandler := &commandRoutingHandler{
		commandType: commandType,
		inner:       baseHandler,
	}

	// 订阅到消息总线
	if err := bus.messageBus.Subscribe(context.Background(), messaging.MessageTypeCommand, routingHandler); err != nil {
		return fmt.Errorf("failed to subscribe command handler: %w", err)
	}

	// 记录处理器
	bus.mutex.Lock()
	bus.handlers[commandType] = routingHandler
	bus.mutex.Unlock()

	return nil
}

// Dispatch 分发命令
//
// 这是命令总线的核心方法，将命令发送到对应的处理器。
//
// 参数：
//   - ctx: 上下文
//   - cmd: 待分发的命令
//
// 返回：
//   - error: 处理失败时返回错误
//
// 执行流程：
//  1. 如果启用了聚合锁，获取聚合级锁
//  2. 委托给 MessageBus.Publish（自动执行中间件链）
//  3. MessageBus 根据消息类型路由到对应的处理器
func (bus *CommandBus) Dispatch(ctx context.Context, cmd *Command) error {
	if cmd == nil {
		return ErrInvalidCommand
	}

	// 命令分发交由底层消息总线和 Transport 决定同步/异步语义
	return bus.messageBus.Publish(ctx, cmd)
}

// Use 注册中间件
//
// 委托给底层 MessageBus，复用中间件机制
func (bus *CommandBus) Use(middleware messaging.IMiddleware) {
	bus.messageBus.Use(middleware)
}

// GetHandler 获取命令处理器（用于测试）
func (bus *CommandBus) GetHandler(commandType string) (messaging.IMessageHandler, bool) {
	bus.mutex.RLock()
	defer bus.mutex.RUnlock()

	handler, exists := bus.handlers[commandType]
	return handler, exists
}

// HasHandler 检查是否已注册处理器
func (bus *CommandBus) HasHandler(commandType string) bool {
	bus.mutex.RLock()
	defer bus.mutex.RUnlock()

	_, exists := bus.handlers[commandType]
	return exists
}
