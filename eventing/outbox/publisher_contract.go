package outbox

import (
	"context"
	"gochen/process/task"
)

// IOutboxPublisher 抽象发件箱Publisher能力接口。
type IOutboxPublisher interface {
	// TaskRegistry 返回实际后台处理函数的注册源，封存并核对后才允许启动。
	TaskRegistry() *task.Registry
	// SetTaskExecutor 在封存前连接消费方的受保护执行入口。
	SetTaskExecutor(TaskExecutor) error
	// Start 启动后台发布任务
	Start(ctx context.Context) error

	// Stop 停止后台发布任务（ctx 用于控制等待上界）。
	Stop(ctx context.Context) error

	// PublishPending 手动触发发布待处理的事件
	PublishPending(ctx context.Context) error
}
