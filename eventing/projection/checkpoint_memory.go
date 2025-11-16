package projection

import (
	"context"
	"sync"
)

// MemoryCheckpointStore 内存检查点存储（用于测试）
//
// 不持久化，进程重启后数据丢失。
// 仅用于开发和测试环境。
type MemoryCheckpointStore struct {
	checkpoints map[string]*Checkpoint
	mutex       sync.RWMutex
}

// NewMemoryCheckpointStore 创建内存检查点存储
func NewMemoryCheckpointStore() *MemoryCheckpointStore {
	return &MemoryCheckpointStore{
		checkpoints: make(map[string]*Checkpoint),
	}
}

// Load 加载检查点
func (s *MemoryCheckpointStore) Load(ctx context.Context, projectionName string) (*Checkpoint, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	checkpoint, exists := s.checkpoints[projectionName]
	if !exists {
		return nil, ErrCheckpointNotFound
	}

	return checkpoint.Clone(), nil
}

// Save 保存检查点
func (s *MemoryCheckpointStore) Save(ctx context.Context, checkpoint *Checkpoint) error {
	if checkpoint == nil || !checkpoint.IsValid() {
		return ErrInvalidCheckpoint
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.checkpoints[checkpoint.ProjectionName] = checkpoint.Clone()
	return nil
}

// Delete 删除检查点
func (s *MemoryCheckpointStore) Delete(ctx context.Context, projectionName string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.checkpoints, projectionName)
	return nil
}

// Clear 清空所有检查点（测试用）
func (s *MemoryCheckpointStore) Clear() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.checkpoints = make(map[string]*Checkpoint)
}

// Count 返回检查点数量（测试用）
func (s *MemoryCheckpointStore) Count() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return len(s.checkpoints)
}

// Ensure MemoryCheckpointStore implements ICheckpointStore
var _ ICheckpointStore = (*MemoryCheckpointStore)(nil)
