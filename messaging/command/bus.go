package command

import (
	"context"
	"fmt"
	"sync"

	"gochen/logging"
	"gochen/messaging"
	"gochen/messaging/transport/memory"
	synctransport "gochen/messaging/transport/sync"
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

	// syncTransport 标记当前 CommandBus 所在的 Transport 是否为“同步执行”语义
	//
	// 说明：
	//   - 当为 true 且底层 Transport 为同步实现（如 transport/sync），Dispatch 返回值通常可以反映 handler 的执行结果；
	//   - 当为 false（典型如 memory/redisstreams/natsjetstream 等异步 Transport），Dispatch 的 error 仅保证传输层是否成功，
	//     handler 的业务错误不会可靠地通过返回值传播。
	syncTransport bool

	logger logging.ILogger
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

	// Logger 为命令总线注入的组件级 logger。
	// 若为空，将在构造函数中基于全局 Logger 派生带 component 字段的默认 logger。
	Logger logging.ILogger
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

	bus := &CommandBus{
		messageBus: messageBus,
		handlers:   make(map[string]messaging.IMessageHandler),
	}

	// 初始化组件级 logger（优先使用显式注入，其次基于全局 Logger 派生）
	if config.Logger != nil {
		bus.logger = config.Logger
	} else {
		bus.logger = logging.ComponentLogger("messaging.command.bus")
	}

	// 尝试探测底层 ITransport 类型，以便给出更明确的错误语义告警
	type transportProvider interface {
		GetTransport() messaging.ITransport
	}

	if provider, ok := messageBus.(transportProvider); ok {
		switch provider.GetTransport().(type) {
		case *synctransport.SyncTransport:
			bus.syncTransport = true
		case *memory.MemoryTransport:
			bus.syncTransport = false
		default:
			// 未知类型：保守假定为异步，并给出一次性告警
			bus.logger.Warn(context.Background(),
				"CommandBus 使用的 Transport 类型未知，Dispatch 错误语义仅保证传输层；业务错误请通过领域返回值或监控钩子处理",
				logging.String("transport_type", fmt.Sprintf("%T", provider.GetTransport())),
			)
		}
	} else {
		// 无法探测 Transport（自定义 IMessageBus 实现），同样给出一次性告警
		bus.logger.Warn(context.Background(),
			"CommandBus 无法探测底层 Transport 类型，Dispatch 错误语义仅保证传输层；请确认所用 IMessageBus/Transport 的同步语义",
		)
	}

	return bus
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
	return bus.RegisterHandlerWithContext(context.Background(), commandType, handler)
}

// RegisterHandlerWithContext 注册命令处理器（支持上下文透传，并替换已存在的处理器）
func (bus *CommandBus) RegisterHandlerWithContext(ctx context.Context, commandType string, handler CommandHandlerFunc) error {
	if ctx == nil {
		ctx = context.Background()
	}

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

	// 若已存在同类型处理器，先移除旧订阅，避免重复消费
	bus.mutex.RLock()
	existing := bus.handlers[commandType]
	bus.mutex.RUnlock()
	if existing != nil {
		if err := bus.messageBus.Unsubscribe(ctx, messaging.MessageTypeCommand, existing); err != nil {
			return fmt.Errorf("failed to replace existing handler for %s: %w", commandType, err)
		}
	}

	// 订阅到消息总线
	if err := bus.messageBus.Subscribe(ctx, messaging.MessageTypeCommand, routingHandler); err != nil {
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
