package outbox

import (
	"context"
	"sync"
	"time"

	"gochen/errors"
	"gochen/eventing/store"
)

var _ IOutboxRepository[int64] = (*MemoryOutboxRepository[int64])(nil)
var _ IOutboxMaxRetriesAware = (*MemoryOutboxRepository[int64])(nil)

// MemoryOutboxRepository 提供基于纯内存的 Outbox 仓储实现。
type MemoryOutboxRepository[ID comparable] struct {
	mu          sync.RWMutex
	eventStore  *store.MemoryEventStore[ID]
	entries     []OutboxEntry[ID]
	eventIDs    map[string]struct{}
	nextID      int64
	nextClaimID uint64
	claimLease  time.Duration
	maxRetries  int
	// maxRetriesSet 区分构造默认值与调用方显式 WithMaxRetries 配置。
	maxRetriesSet bool
}

// NewMemoryOutboxRepository 创建纯内存 Outbox 仓储。
//
// eventStore 必须与 DomainEventStore 使用同一个实例，避免事件写入后从另一份
// 内存存储恢复聚合。构造函数不创建隐藏的事件存储。
func NewMemoryOutboxRepository[ID comparable](eventStore *store.MemoryEventStore[ID]) (*MemoryOutboxRepository[ID], error) {
	if eventStore == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event store cannot be nil")
	}
	return &MemoryOutboxRepository[ID]{
		eventStore: eventStore,
		entries:    make([]OutboxEntry[ID], 0),
		eventIDs:   make(map[string]struct{}),
		nextID:     1,
		claimLease: defaultClaimLease,
		maxRetries: DefaultMaxRetries,
	}, nil
}

// WithMaxRetries 配置最大重试次数。
func (m *MemoryOutboxRepository[ID]) WithMaxRetries(maxRetries int) *MemoryOutboxRepository[ID] {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxRetries = maxRetries
	m.maxRetriesSet = true
	return m
}

// ConfiguredMaxRetries 返回显式配置的重试上限；maxRetriesSet 区分构造默认值、
// 显式 WithMaxRetries 与 publisher 下发（ApplyMaxRetries）三种来源。
func (m *MemoryOutboxRepository[ID]) ConfiguredMaxRetries() (int, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxRetries, m.maxRetriesSet
}

// ApplyMaxRetries 接受 publisher 下发的重试上限，并视为已完成对齐
// （此后 ConfiguredMaxRetries 报告 configured=true，后续 publisher 采纳而非覆盖）。
func (m *MemoryOutboxRepository[ID]) ApplyMaxRetries(maxRetries int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxRetries = maxRetries
	m.maxRetriesSet = true
}

func (m *MemoryOutboxRepository[ID]) initializeLocked() {
	if m.eventIDs == nil {
		m.eventIDs = make(map[string]struct{})
		for i := range m.entries {
			m.eventIDs[m.entries[i].EventID] = struct{}{}
		}
	}
	if m.nextID <= 0 {
		m.nextID = 1
		for i := range m.entries {
			if m.entries[i].ID >= m.nextID {
				m.nextID = m.entries[i].ID + 1
			}
		}
	}
}

func memoryOutboxContextError(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx.Err()
}
