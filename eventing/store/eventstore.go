package store

import (
	"context"
	"time"

	"gochen/errors"
	"gochen/eventing"
)

// NewStreamNotSupportedError 构造一个带操作名上下文的“不支持流式访问”错误。
func NewStreamNotSupportedError(operation string) error {
	if operation == "" {
		operation = "stream"
	}
	return errors.NewCode(errors.Unsupported, "stream operation not supported").
		WithContext("operation", operation)
}

// IsStreamNotSupported 判断错误是否表示某类流式操作不受支持。
func IsStreamNotSupported(err error, operation string) bool {
	if err == nil {
		return false
	}
	if !errors.Is(err, errors.Unsupported) {
		return false
	}
	if operation == "" {
		return true
	}
	var appErr *errors.AppError
	if !errors.As(err, &appErr) {
		return false
	}
	details := appErr.Details()
	op, ok := details["operation"].(string)
	return ok && op == operation
}

// IEventStore 提供聚合事件的完整读写能力。
// expectedVersion 为上次提交版本，新聚合为 0；版本检查与追加必须原子执行。
// 流仍位于该批次末尾时，相同事件的完整重试应幂等成功；不承诺过期历史批次的重试成功。
// 不同事件占用同一版本返回 Concurrency；重复事件 ID 的内容冲突必须拒绝。
// 只读消费者应依赖 IEventReader、IAggregateEventReader 或 IEventStreamReader。
type IEventStore[ID comparable] interface {
	IEventReader[ID]
	// AppendEvents 按预期版本原子追加完整批次。
	AppendEvents(ctx context.Context, aggregateType string, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error
	// HasAggregate 检查指定聚合事件流是否存在。
	HasAggregate(ctx context.Context, aggregateType string, aggregateID ID) (bool, error)
}

// IEventStreamStore 组合完整聚合读写与两种扫描能力，供完整后端和缓存装饰器使用。
// 恢复、历史和投影消费者各自依赖最小读取契约，不要求实现这个组合接口。
type IEventStreamStore[ID comparable] interface {
	IEventStore[ID]
	IAggregateEventReader[ID]
	IEventStreamReader[ID]
}

// StreamOptions 全局事件流查询选项。
type StreamOptions struct {
	After          string    // 游标，从该位置之后开始查询
	Limit          int       // 限制返回数量
	Types          []string  // 事件类型过滤
	FromTime       time.Time // 起始时间（包含）
	ToTime         time.Time // 结束时间（包含）
	AggregateTypes []string  // 聚合类型过滤
}

// DefaultStreamLimit 默认事件流 limit。
//
// 用于避免调用方未设置 Limit 时一次性加载过多事件导致内存压力。
const DefaultStreamLimit = 1000

// MaxStreamLimit 限制调用方一次可请求的最大事件数，避免单次查询放大 SQL 与内存开销。
const MaxStreamLimit = 1000

// NormalizeStreamLimit 统一规范化事件流 limit。
func NormalizeStreamLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultStreamLimit
	case limit > MaxStreamLimit:
		return MaxStreamLimit
	default:
		return limit
	}
}

// StreamResult 全局事件流查询结果。
type StreamResult[ID comparable] struct {
	Events     []eventing.Event[ID] `json:"events"`                // 事件列表
	NextCursor string               `json:"next_cursor,omitempty"` // 下一页游标
	// EventCursors 与 Events 一一对应，表示每条事件成功处理后可持久化的后继游标。
	// 为空时调用方可回退到事件 ID；使用非 ID 游标的存储实现应填充该字段。
	EventCursors []string `json:"event_cursors,omitempty"`
	HasMore      bool     `json:"has_more,omitempty"` // 是否有更多数据
}

// AggregateStreamOptions 单个聚合事件流查询选项。
//
// 设计原则：
// - 仅包含与存储层紧密相关的技术字段（聚合标识 + 版本 + 限制）
// - 不承载任何业务分页语义（page/pageSize），避免 EventStore 被上层污染。
type AggregateStreamOptions[ID comparable] struct {
	AggregateType string // 聚合类型
	AggregateID   ID     // 聚合ID
	AfterVersion  uint64 // 起始版本号（不包含）
	Limit         int    // 限制返回数量
}

// AggregateStreamResult 单个聚合事件流查询结果。
type AggregateStreamResult[ID comparable] struct {
	Events      []eventing.Event[ID] // 事件列表
	NextVersion uint64               // 下一个版本号
	HasMore     bool                 // 是否有更多数据
}

// EventAppend 表示一个聚合事件流的追加请求。
// Events 为空时只严格校验 ExpectedVersion，与本批写入共同构成可串行化事务。
type EventAppend[ID comparable] struct {
	AggregateType   string
	AggregateID     ID
	Events          []eventing.IStorableEvent[ID]
	ExpectedVersion uint64
}

// IAtomicEventStore 支持跨聚合事件流的原子追加。
// 任一项校验、并发检查或写入失败时，整批不得产生可见的部分写入。
// 单项版本与幂等语义与 AppendEvents 相同；空批次不写入。
type IAtomicEventStore[ID comparable] interface {
	// AppendEventsBatch 在同一原子边界内校验并追加多个聚合事件流。
	AppendEventsBatch(ctx context.Context, batch []EventAppend[ID]) error
}
