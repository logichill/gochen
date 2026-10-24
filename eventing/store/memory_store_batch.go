package store

import (
	"context"
	"maps"
	"slices"
	"strings"

	"gochen/errors"
)

// AppendEventsBatch 在私有副本中验证并追加整批事件，成功后在锁内替换可见状态。
// 内存实现仅供测试和示例使用，此操作复制索引及事件切片以隔离失败批次。
func (m *MemoryEventStore[ID]) AppendEventsBatch(ctx context.Context, batch []EventAppend[ID]) error {
	if err := memoryEventStoreContextError(ctx); err != nil {
		return err
	}
	if m == nil {
		return errors.NewCode(errors.InvalidInput, "memory event store is nil")
	}
	if len(batch) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	staged := NewMemoryEventStore[ID]()
	for key, events := range m.events {
		staged.events[key] = slices.Clone(events)
	}
	staged.eventByID = maps.Clone(m.eventByID)
	staged.globalEvents = slices.Clone(m.globalEvents)
	for _, entry := range batch {
		if len(entry.Events) == 0 {
			typ := strings.TrimSpace(entry.AggregateType)
			if typ == "" {
				return errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
			}
			version, err := staged.GetAggregateVersion(ctx, typ, entry.AggregateID)
			if err != nil {
				return err
			}
			if version != entry.ExpectedVersion {
				return errors.NewCode(errors.Concurrency, "atomic read version changed").WithContext("aggregate_id", entry.AggregateID).WithContext("expected_version", entry.ExpectedVersion).WithContext("actual_version", version)
			}
			continue
		}

		if err := staged.AppendEvents(ctx, entry.AggregateType, entry.AggregateID, entry.Events, entry.ExpectedVersion); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.events, m.eventByID, m.globalEvents = staged.events, staged.eventByID, staged.globalEvents
	return nil
}
