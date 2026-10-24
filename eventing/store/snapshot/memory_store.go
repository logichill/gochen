package snapshot

import (
	"context"
	"sort"
	"sync"
	"time"

	"gochen/errors"
	"gochen/observe/logging"
)

// MemoryStore 使用内存 map 保存快照，适合测试和轻量演示场景。
type MemoryStore[ID comparable] struct {
	snapshots map[snapshotKey[ID]]Snapshot[ID]
	mutex     sync.RWMutex
	log       logging.ILogger
}

// NewMemoryStore 创建一个空的内存快照存储。
func NewMemoryStore[ID comparable]() *MemoryStore[ID] {
	return NewMemoryStoreWithLogger[ID](nil)
}

// NewMemoryStoreWithLogger 创建一个复用显式基础 logger 的内存快照存储。
func NewMemoryStoreWithLogger[ID comparable](logger logging.ILogger) *MemoryStore[ID] {
	return &MemoryStore[ID]{
		snapshots: make(map[snapshotKey[ID]]Snapshot[ID]),
		log:       newSnapshotLogger(logger),
	}
}

// SaveSnapshot 覆盖保存一条聚合快照。
func (s *MemoryStore[ID]) SaveSnapshot(ctx context.Context, snapshot Snapshot[ID]) error {
	snapshot = cloneSnapshot(snapshot)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	key := newSnapshotKey(snapshot.AggregateType, snapshot.AggregateID)
	s.snapshots[key] = snapshot
	snapshotStoreLogger(s.log).Debug(ctx, "snapshot saved",
		logging.Any("aggregate_id", snapshot.AggregateID),
		logging.Any("version", snapshot.Version))
	return nil
}

// FindSnapshot 读取指定聚合的最新快照。
func (s *MemoryStore[ID]) FindSnapshot(ctx context.Context, aggregateType string, aggregateID ID) (*Snapshot[ID], error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	key := newSnapshotKey(aggregateType, aggregateID)
	snapshot, ok := s.snapshots[key]
	if !ok {
		return nil, errors.NewCode(errors.NotFound, "snapshot not found").
			WithContext("aggregate_type", aggregateType).
			WithContext("aggregate_id", aggregateID)
	}
	snapshot = cloneSnapshot(snapshot)
	return &snapshot, nil
}

// DeleteSnapshot 删除指定聚合的快照。
func (s *MemoryStore[ID]) DeleteSnapshot(ctx context.Context, aggregateType string, aggregateID ID) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.snapshots, newSnapshotKey(aggregateType, aggregateID))
	snapshotStoreLogger(s.log).Debug(ctx, "snapshot deleted",
		logging.Any("aggregate_id", aggregateID))
	return nil
}

// ListSnapshots 列出匹配聚合类型的快照，并按时间倒序返回。
func (s *MemoryStore[ID]) ListSnapshots(ctx context.Context, aggregateType string, limit int) ([]Snapshot[ID], error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// 先收集所有匹配的快照
	var result []Snapshot[ID]
	for _, ss := range s.snapshots {
		if aggregateType == "" || ss.AggregateType == aggregateType {
			result = append(result, cloneSnapshot(ss))
		}
	}

	// 按时间戳降序排序，确保结果顺序一致
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.After(result[j].Timestamp)
	})

	// 应用 limit
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// CleanupSnapshots 删除超过保留期限的快照。
func (s *MemoryStore[ID]) CleanupSnapshots(ctx context.Context, retentionPeriod time.Duration) error {
	if retentionPeriod <= 0 {
		return nil
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	cutoff := time.Now().Add(-retentionPeriod)
	deleted := 0
	for k, v := range s.snapshots {
		if v.Timestamp.Before(cutoff) {
			delete(s.snapshots, k)
			deleted++
		}
	}
	snapshotStoreLogger(s.log).Info(ctx, "expired snapshots cleaned up", logging.Int("deleted_count", deleted))
	return nil
}
