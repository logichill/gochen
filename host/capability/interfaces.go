package capability

import (
	"context"

	"gochen/eventing/projection"
	"gochen/messaging"
)

// IEventSubscriber 表示模块事件处理器所需的订阅能力。
type IEventSubscriber interface {
	Subscribe(ctx context.Context, messageType string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error)
}

// ITransport 表示 Host 负责启动/停止的消息传输能力。
type ITransport interface {
	IEventSubscriber
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// IProjectionManager 表示模块投影注册能力。
type IProjectionManager interface {
	projection.IProjectionRegistrar
}

// IProjectionStarter 表示投影支持显式启动。
type IProjectionStarter interface {
	StartProjection(name string) error
}

// IProjectionStopper 表示投影支持显式停止。
type IProjectionStopper interface {
	StopProjection(name string) error
}

// IRuntimeComponent 定义需要由模块生命周期托管启动的运行期组件。
type IRuntimeComponent interface {
	Start(ctx context.Context) error
}

// IRuntimeStopper 表示运行期组件支持优雅停止。
type IRuntimeStopper interface {
	Stop(ctx context.Context) error
}

// IMessageTypesProvider 允许处理器声明自己关心的多个消息类型。
type IMessageTypesProvider interface {
	EventTypes() []string
}
