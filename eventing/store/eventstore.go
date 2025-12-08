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
//
// 类型参数：
//   - ID: 聚合根 ID 类型，必须是可比较类型（如 int64、string、uuid 等）
type IEventStore[ID comparable] interface {
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
	AppendEvents(ctx context.Context, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error

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
	LoadEvents(ctx context.Context, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error)

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
	StreamEvents(ctx context.Context, fromTime time.Time) ([]eventing.Event[ID], error)
}

// IAggregateInspector 定义聚合检查接口（可选扩展）
//
// 提供聚合存在性检查和版本查询功能，用于优化某些业务场景。
type IAggregateInspector[ID comparable] interface {
	// HasAggregate 检查指定聚合是否存在
	//
	// 返回：
	//   - bool: true表示聚合存在
	//   - error: 查询失败时返回错误
	HasAggregate(ctx context.Context, aggregateID ID) (bool, error)

	// GetAggregateVersion 获取聚合的当前版本号
	//
	// 返回：
	//   - uint64: 版本号，0表示聚合不存在
	//   - error: 查询失败时返回错误
	GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error)
}

// ITypedEventStore 定义按类型加载事件的接口（可选扩展）
//
// 支持按聚合类型过滤事件，用于多租户或多聚合类型场景。
type ITypedEventStore[ID comparable] interface {
	// LoadEventsByType 按聚合类型加载事件
	LoadEventsByType(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error)
}

// IEventStreamStore 全局事件流存储接口。
//
// 用途：
// - 为投影管理器、历史查询、监控等需要全局扫描的场景提供流式访问能力；
// - 支持游标分页和聚合顺序流两种扫描模式；
// - 相比基础的 IEventStore，增加了高效的批量查询和流式处理能力。
//
// 设计原则：
// - 聚合级操作（Append/Load）依赖基础 IEventStore[ID]；
// - 全局流查询功能集中在本接口，避免概念混淆。
type IEventStreamStore[ID comparable] interface {
	IEventStore[ID]

	// GetEventStreamWithCursor 使用游标获取全局事件流。
	//
	// 用途：
	//   - 投影重建：按时间窗口批量加载事件
	//   - 事件归档：分页导出历史事件
	//   - 监控分析：按类型过滤事件流
	//
	// 参数：
	//   - ctx: 上下文
	//   - opts: 查询选项（游标、限制、过滤条件等）
	//
	// 返回：
	//   - *StreamResult: 查询结果（事件列表、下一页游标、是否有更多数据）
	//   - error: 查询失败时返回错误
	GetEventStreamWithCursor(ctx context.Context, opts *StreamOptions) (*StreamResult[ID], error)

	// StreamAggregate 按聚合顺序流式读取事件。
	//
	// 用途：
	//   - 为上层 Repository/Service 提供更高效的"按版本片段读取"能力
	//   - 避免在应用层一次性加载聚合全部事件再做内存分页
	//
	// 参数：
	//   - ctx: 上下文
	//   - opts: 聚合流查询选项（聚合标识 + 版本 + 限制）
	//
	// 返回：
	//   - *AggregateStreamResult: 查询结果（事件列表、下一版本号、是否有更多数据）
	//   - error: 查询失败时返回错误
	//
	// 注意：
	//   - 该接口不关心业务分页语义（page/pageSize），仅提供基于版本的 limit/游标能力
	//   - 业务分页应在仓储或 Service 层基于 NextVersion/HasMore 实现
	StreamAggregate(ctx context.Context, opts *AggregateStreamOptions[ID]) (*AggregateStreamResult[ID], error)
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

// StreamResult 全局事件流查询结果。
type StreamResult[ID comparable] struct {
	Events     []eventing.Event[ID] // 事件列表
	NextCursor string               // 下一页游标
	HasMore    bool                 // 是否有更多数据
}

// AggregateStreamOptions 单个聚合事件流查询选项。
//
// 设计原则：
// - 仅包含与存储层紧密相关的技术字段（聚合标识 + 版本 + 限制）
// - 不承载任何业务分页语义（page/pageSize），避免 EventStore 被上层污染
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
