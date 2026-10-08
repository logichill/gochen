package outbox

import (
	"context"
)

// IOutboxPublisher 抽象发件箱Publisher能力接口。
type IOutboxPublisher interface {
	// Start 启动后台发布任务
	Start(ctx context.Context) error

	// Stop 停止后台发布任务（ctx 用于控制等待上界）。
	Stop(ctx context.Context) error

	// PublishPending 手动触发发布待处理的事件
	PublishPending(ctx context.Context) error
}
