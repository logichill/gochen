package store

import (
	"context"
	"sync"

	"gochen/errors"
	"gochen/eventing"
)

// MemoryEventStore 是支持任意 comparable 聚合 ID 的内存事件存储，仅用于测试与示例。
//
// 说明：
// - 聚合身份由 aggregateType + aggregateID 定义。
type MemoryEventStore[ID comparable] struct {
	mu sync.RWMutex
	// events 按 aggregateType + aggregateID 维度组织。
	events map[memoryAggregateKey[ID]][]eventing.Event[ID]
	// eventByID 按事件 ID 维护全局唯一索引，保持与 SQL store 的 event_id 唯一约束一致。
	eventByID map[string]eventing.Event[ID]
	// globalEvents 按 timestamp/id 维护全局事件流，避免 StreamEvents 每次全量排序。
	globalEvents []eventing.Event[ID]
}

// NewMemoryEventStore 创建一个仅供测试和示例使用的内存事件存储。
func NewMemoryEventStore[ID comparable]() *MemoryEventStore[ID] {
	return &MemoryEventStore[ID]{
		events:    make(map[memoryAggregateKey[ID]][]eventing.Event[ID]),
		eventByID: make(map[string]eventing.Event[ID]),
	}
}

// NewMemoryAggregateStore 返回可作为 IEventStore[ID] 使用的内存事件存储。
func NewMemoryAggregateStore[ID comparable]() *MemoryEventStore[ID] {
	return NewMemoryEventStore[ID]()
}

// NewMemoryEventStreamStore 返回可作为 IEventStreamStore[ID] 使用的内存事件存储。
func NewMemoryEventStreamStore[ID comparable]() *MemoryEventStore[ID] {
	return NewMemoryEventStore[ID]()
}

func memoryEventStoreContextError(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx.Err()
}

// 编译期断言：确保泛型内存存储实现事件流接口。
var _ IEventStreamStore[int64] = (*MemoryEventStore[int64])(nil)

var _ IEventStreamStore[string] = (*MemoryEventStore[string])(nil)
