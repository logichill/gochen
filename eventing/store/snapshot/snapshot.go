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
type Snapshot struct {
	AggregateID   int64          `json:"aggregate_id"`
	AggregateType string         `json:"aggregate_type"`
	Version       uint64         `json:"version"`
	Data          []byte         `json:"data"`
	Timestamp     time.Time      `json:"timestamp"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// ISnapshotStore 快照存储接口
type ISnapshotStore interface {
	SaveSnapshot(ctx context.Context, snapshot Snapshot) error
	GetSnapshot(ctx context.Context, aggregateType string, aggregateID int64) (*Snapshot, error)
	DeleteSnapshot(ctx context.Context, aggregateType string, aggregateID int64) error
	GetSnapshots(ctx context.Context, aggregateType string, limit int) ([]Snapshot, error)
	CleanupSnapshots(ctx context.Context, retentionPeriod time.Duration) error
}

// MemoryStore 内存快照存储
type MemoryStore struct {
	snapshots map[string]Snapshot
	mutex     sync.RWMutex
}

// NewMemoryStore 创建内存快照存储
func NewMemoryStore() *MemoryStore { return &MemoryStore{snapshots: make(map[string]Snapshot)} }

func snapshotKey(aggregateType string, aggregateID int64) string {
	return fmt.Sprintf("%s:%d", aggregateType, aggregateID)
}

// SaveSnapshot 保存快照
func (s *MemoryStore) SaveSnapshot(ctx context.Context, snapshot Snapshot) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	key := snapshotKey(snapshot.AggregateType, snapshot.AggregateID)
	s.snapshots[key] = snapshot
	snapshotLogger().Debug(ctx, "[ISnapshotStore] 保存快照", logging.Int64("aggregate_id", snapshot.AggregateID), logging.Any("version", snapshot.Version))
	return nil
}

// GetSnapshot 获取快照
func (s *MemoryStore) GetSnapshot(ctx context.Context, aggregateType string, aggregateID int64) (*Snapshot, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	key := snapshotKey(aggregateType, aggregateID)
	snapshot, ok := s.snapshots[key]
	if !ok {
		return nil, fmt.Errorf("snapshot not found for aggregate %d", aggregateID)
	}
	return &snapshot, nil
}

// DeleteSnapshot 删除快照
func (s *MemoryStore) DeleteSnapshot(ctx context.Context, aggregateType string, aggregateID int64) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.snapshots, snapshotKey(aggregateType, aggregateID))
	snapshotLogger().Debug(ctx, "[ISnapshotStore] 删除快照", logging.Int64("aggregate_id", aggregateID))
	return nil
}

// GetSnapshots 获取快照列表
//
// 返回结果按 Timestamp 降序排列（最新的在前），确保结果顺序一致。
func (s *MemoryStore) GetSnapshots(ctx context.Context, aggregateType string, limit int) ([]Snapshot, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// 先收集所有匹配的快照
	var result []Snapshot
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
func (s *MemoryStore) CleanupSnapshots(ctx context.Context, retentionPeriod time.Duration) error {
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
