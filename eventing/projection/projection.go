package projection

import (
	"context"
	"time"

	"gochen/eventing"
)

// IProjection 投影接口。
type IProjection[ID comparable] interface {
	// 获取投影名称
	Name() string

	// 处理事件
	Handle(ctx context.Context, event eventing.IEvent) error

	// 获取支持的事件类型
	SupportedEventTypes() []string

	// 重建投影
	Rebuild(ctx context.Context, events []eventing.Event[ID]) error

	// 获取投影状态
	Status() ProjectionStatus
}

// ProjectionStatus 投影状态。
type ProjectionStatus struct {
	Name            string    `json:"name"`
	LastEventID     string    `json:"last_event_id"`
	LastEventTime   time.Time `json:"last_event_time"`
	ProcessedEvents int64     `json:"processed_events"`
	FailedEvents    int64     `json:"failed_events"`
	Status          string    `json:"status"` // running, stopped, error
	LastError       string    `json:"last_error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
