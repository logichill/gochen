package saga

import (
	"context"
	"sync"
)

// MemorySagaStateStore 内存 Saga 状态存储（用于测试）
//
// 不持久化，进程重启后数据丢失。
// 仅用于开发和测试环境。
type MemorySagaStateStore struct {
	states map[string]*SagaState
	mutex  sync.RWMutex
}

// NewMemorySagaStateStore 创建内存状态存储
func NewMemorySagaStateStore() *MemorySagaStateStore {
	return &MemorySagaStateStore{
		states: make(map[string]*SagaState),
	}
}

// Load 加载状态
func (s *MemorySagaStateStore) Load(ctx context.Context, sagaID string) (*SagaState, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	state, exists := s.states[sagaID]
	if !exists {
		return nil, ErrSagaNotFound
	}

	return state.Clone(), nil
}

// Save 保存状态
func (s *MemorySagaStateStore) Save(ctx context.Context, state *SagaState) error {
	if state == nil || state.SagaID == "" {
		return ErrSagaInvalidState
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.states[state.SagaID] = state.Clone()
	return nil
}

// Update 更新状态
func (s *MemorySagaStateStore) Update(ctx context.Context, state *SagaState) error {
	return s.Save(ctx, state)
}

// Delete 删除状态
func (s *MemorySagaStateStore) Delete(ctx context.Context, sagaID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.states, sagaID)
	return nil
}

// List 列出状态
func (s *MemorySagaStateStore) List(ctx context.Context, status SagaStatus) ([]*SagaState, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var result []*SagaState
	for _, state := range s.states {
		if status == "" || state.Status == status {
			result = append(result, state.Clone())
		}
	}

	return result, nil
}

// Clear 清空所有状态（测试用）
func (s *MemorySagaStateStore) Clear() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.states = make(map[string]*SagaState)
}

// Count 返回状态数量（测试用）
func (s *MemorySagaStateStore) Count() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return len(s.states)
}

// Ensure MemorySagaStateStore implements ISagaStateStore
var _ ISagaStateStore = (*MemorySagaStateStore)(nil)
