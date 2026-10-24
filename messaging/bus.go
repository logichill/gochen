// Package messaging 提供消息总线的核心功能，包含基础的消息发布、订阅、路由和处理机制。
package messaging

import (
	"reflect"
	"sync"
)

// MessageBus 消息总线基础实现。
// 它依赖于 ITransport 接口来处理实际的消息传输，并支持中间件。
type MessageBus struct {
	transport   ITransport
	middlewares []IMiddleware
	mutex       sync.RWMutex

	// handlerErrorHook 可选的处理器错误钩子
	handlerErrorHook HandlerErrorHook
}

// NewMessageBus 创建一个基于指定 transport 的消息总线。
func NewMessageBus(transport ITransport) *MessageBus {
	return &MessageBus{
		transport:   transport,
		middlewares: make([]IMiddleware, 0),
	}
}

// Transport 返回底层传输实现。
//
// 该方法主要给装配或桥接代码使用；业务层应尽量只依赖 IMessageBus 抽象。
func (bus *MessageBus) Transport() ITransport {
	return bus.transport
}

// Use 追加一层消息总线中间件。
func (bus *MessageBus) Use(middleware IMiddleware) {
	// 除普通 nil 外还识别 typed-nil（如未初始化的中间件指针），
	// 避免装配期漏拦、发布期执行中间件链时 nil 解引用 panic。
	if middleware == nil || isNilValue(reflect.ValueOf(middleware)) {
		return
	}
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	bus.middlewares = append(bus.middlewares, middleware)
}

func (bus *MessageBus) snapshotMiddlewares() []IMiddleware {
	bus.mutex.RLock()
	defer bus.mutex.RUnlock()
	return append([]IMiddleware(nil), bus.middlewares...)
}
