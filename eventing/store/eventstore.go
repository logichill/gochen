package store

import (
	"context"
	"time"

	"gochen/eventing"
)

// IEventStore 定义事件存储的核心接口（最小化设计）
//
// 事件存储是事件溯源架构的核心组件，负责持久化和检索领域事件。
// 该接口遵循最小化原则，仅包含必需的方法。
//
// 核心方法：
//   - AppendEvents: 追加事件到聚合的事件流，支持乐观锁并发控制
//   - LoadEvents: 加载聚合的事件历史，支持增量加载
//   - StreamEvents: 流式读取事件，用于事件重放和投影更新
//
// 最佳实践：
//   - 实现应保证事件的原子性和持久性（ACID）
//   - 使用乐观锁（expectedVersion）防止并发冲突
//   - 支持幂等性，重复追加相同事件应被忽略
type IEventStore interface {
	// AppendEvents 追加事件到指定聚合的事件流
	//
	// 参数：
	//   - ctx: 上下文，用于超时控制和取消
	//   - aggregateID: 聚合根ID
	//   - events: 待追加的事件列表
	//   - expectedVersion:
	//       - 表示当前持久化事件流的“上一次已提交版本号”
	//       - 0 表示新聚合（尚无任何事件被持久化）
	//       - 实现应将其与存储中的当前版本做精确比较，用于乐观锁控制
	//
	// 返回：
	//   - error: 版本冲突返回 ConcurrencyError，其他错误返回 EventStoreError
	AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent, expectedVersion uint64) error

	// LoadEvents 加载聚合的事件历史
	//
	// 参数：
	//   - ctx: 上下文
	//   - aggregateID: 聚合根ID
	//   - afterVersion: 起始版本号（不包含该版本），0表示从头加载
	//
	// 返回：
	//   - []Event: 事件列表，按版本号升序排列
	//   - error: 加载失败时返回错误
	LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event, error)

	// StreamEvents 流式读取指定时间之后的所有事件
	//
	// 用于事件重放、投影更新、事件订阅等场景。
	//
	// 参数：
	//   - ctx: 上下文
	//   - fromTime: 起始时间（包含）
	//
	// 返回：
	//   - []Event: 事件列表，按时间戳升序排列
	//   - error: 读取失败时返回错误
	StreamEvents(ctx context.Context, fromTime time.Time) ([]eventing.Event, error)
}

// IAggregateInspector 定义聚合检查接口（可选扩展）
//
// 提供聚合存在性检查和版本查询功能，用于优化某些业务场景。
type IAggregateInspector interface {
	// HasAggregate 检查指定聚合是否存在
	//
	// 返回：
	//   - bool: true表示聚合存在
	//   - error: 查询失败时返回错误
	HasAggregate(ctx context.Context, aggregateID int64) (bool, error)

	// GetAggregateVersion 获取聚合的当前版本号
	//
	// 返回：
	//   - uint64: 版本号，0表示聚合不存在
	//   - error: 查询失败时返回错误
	GetAggregateVersion(ctx context.Context, aggregateID int64) (uint64, error)
}

// ITypedEventStore 定义按类型加载事件的接口（可选扩展）
//
// 支持按聚合类型过滤事件，用于多租户或多聚合类型场景。
type ITypedEventStore interface {
	// LoadEventsByType 按聚合类型加载事件
	LoadEventsByType(ctx context.Context, aggregateType string, aggregateID int64, afterVersion uint64) ([]eventing.Event, error)
}

// IEventStoreExtended 扩展事件存储接口，提供游标分页能力
//
// 支持高级查询功能，包括：
//   - 游标分页：高效处理大量事件
//   - 类型过滤：按事件类型或聚合类型筛选
//   - 时间范围：指定时间窗口查询
type IEventStoreExtended interface {
	IEventStore

	// GetEventStreamWithCursor 使用游标获取事件流
	//
	// 参数：
	//   - ctx: 上下文
	//   - opts: 查询选项，包括游标、限制、过滤条件等
	//
	// 返回：
	//   - *StreamResult: 查询结果，包含事件列表、下一页游标、是否有更多数据
	//   - error: 查询失败时返回错误
	GetEventStreamWithCursor(ctx context.Context, opts *StreamOptions) (*StreamResult, error)
}

// StreamOptions 事件流查询选项
type StreamOptions struct {
	After          string
	Limit          int
	Types          []string
	FromTime       time.Time
	ToTime         time.Time
	AggregateTypes []string
}

// StreamResult 事件流查询结果
type StreamResult struct {
	Events     []eventing.Event
	NextCursor string
	HasMore    bool
}

// AggregateStreamOptions 单个聚合事件流查询选项（可选扩展）
//
// 设计原则：
// - 仅包含与存储层紧密相关的技术字段（聚合标识 + 版本 + 限制）；
// - 不承载任何业务分页语义（page/pageSize），避免 EventStore 被上层污染。
type AggregateStreamOptions struct {
	AggregateType string
	AggregateID   int64
	AfterVersion  uint64
	Limit         int
}

// AggregateStreamResult 单个聚合事件流查询结果
type AggregateStreamResult struct {
	Events      []eventing.Event
	NextVersion uint64
	HasMore     bool
}

// IAggregateEventStore 按聚合顺序流式读取事件的可选扩展接口
//
// 用途：
// - 为上层 Repository/Service 提供更高效的“按版本片段读取”能力；
// - 避免在应用层一次性加载聚合全部事件再做内存分页。
//
// 注意：
// - 该接口不关心业务分页语义，仅提供基于版本的 limit/游标能力；
// - page/pageSize 等应在仓储或 Service 层基于 NextVersion/HasMore 实现。
type IAggregateEventStore interface {
	StreamAggregate(ctx context.Context, opts *AggregateStreamOptions) (*AggregateStreamResult, error)
}
