package snapshot

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"gochen/logging"
)

func snapshotLogger() logging.ILogger {
	return logging.ComponentLogger("eventstore.snapshot")
}

// Snapshot 定义聚合快照
type Snapshot[ID comparable] struct {
	AggregateID   ID             `json:"aggregate_id"`
	AggregateType string         `json:"aggregate_type"`
	Version       uint64         `json:"version"`
	Data          []byte         `json:"data"`
	Timestamp     time.Time      `json:"timestamp"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// ISnapshotStore 快照存储接口
type ISnapshotStore[ID comparable] interface {
	SaveSnapshot(ctx context.Context, snapshot Snapshot[ID]) error
	GetSnapshot(ctx context.Context, aggregateType string, aggregateID ID) (*Snapshot[ID], error)
	DeleteSnapshot(ctx context.Context, aggregateType string, aggregateID ID) error
	GetSnapshots(ctx context.Context, aggregateType string, limit int) ([]Snapshot[ID], error)
	CleanupSnapshots(ctx context.Context, retentionPeriod time.Duration) error
}

// MemoryStore 内存快照存储
type MemoryStore[ID comparable] struct {
	snapshots map[string]Snapshot[ID]
	mutex     sync.RWMutex
}

// NewMemoryStore 创建内存快照存储
func NewMemoryStore[ID comparable]() *MemoryStore[ID] {
	return &MemoryStore[ID]{snapshots: make(map[string]Snapshot[ID])}
}

func snapshotKey[ID comparable](aggregateType string, aggregateID ID) string {
	return fmt.Sprintf("%s:%v", aggregateType, aggregateID)
}

// SaveSnapshot 保存快照
func (s *MemoryStore[ID]) SaveSnapshot(ctx context.Context, snapshot Snapshot[ID]) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	key := snapshotKey(snapshot.AggregateType, snapshot.AggregateID)
	s.snapshots[key] = snapshot
	snapshotLogger().Debug(ctx, "[ISnapshotStore] 保存快照",
		logging.Any("aggregate_id", snapshot.AggregateID),
		logging.Any("version", snapshot.Version))
	return nil
}

// GetSnapshot 获取快照
func (s *MemoryStore[ID]) GetSnapshot(ctx context.Context, aggregateType string, aggregateID ID) (*Snapshot[ID], error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	key := snapshotKey(aggregateType, aggregateID)
	snapshot, ok := s.snapshots[key]
	if !ok {
		return nil, fmt.Errorf("snapshot not found for aggregate %v", aggregateID)
	}
	return &snapshot, nil
}

// DeleteSnapshot 删除快照
func (s *MemoryStore[ID]) DeleteSnapshot(ctx context.Context, aggregateType string, aggregateID ID) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.snapshots, snapshotKey(aggregateType, aggregateID))
	snapshotLogger().Debug(ctx, "[ISnapshotStore] 删除快照",
		logging.Any("aggregate_id", aggregateID))
	return nil
}

// GetSnapshots 获取快照列表
//
// 返回结果按 Timestamp 降序排列（最新的在前），确保结果顺序一致。
func (s *MemoryStore[ID]) GetSnapshots(ctx context.Context, aggregateType string, limit int) ([]Snapshot[ID], error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// 先收集所有匹配的快照
	var result []Snapshot[ID]
	for _, ss := range s.snapshots {
		if aggregateType == "" || ss.AggregateType == aggregateType {
			result = append(result, ss)
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

// CleanupSnapshots 清理过期快照
func (s *MemoryStore[ID]) CleanupSnapshots(ctx context.Context, retentionPeriod time.Duration) error {
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
	snapshotLogger().Info(ctx, "[ISnapshotStore] 清理过期快照", logging.Int("deleted_count", deleted))
	return nil
}
