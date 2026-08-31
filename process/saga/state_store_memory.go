package saga

import (
	"context"
	"sync"

	"gochen/errors"
)

// MemorySagaStateStore 定义MemorySaga状态存储。
type MemorySagaStateStore struct {
	states map[string]*SagaState
	mutex  sync.RWMutex
}

// NewMemorySagaStateStore 创建MemorySaga状态存储。
func NewMemorySagaStateStore() *MemorySagaStateStore {
	return &MemorySagaStateStore{
		states: make(map[string]*SagaState),
	}
}

// Load 加载数据。
func (s *MemorySagaStateStore) Load(ctx context.Context, sagaID string) (*SagaState, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	state, exists := s.states[sagaID]
	if !exists {
		return nil, errors.NewCode(errors.NotFound, "saga not found").WithContext("saga_id", sagaID)
	}

	return state.Clone(), nil
}

// Save 保存状态。
func (s *MemorySagaStateStore) Save(ctx context.Context, state *SagaState) error {
	if state == nil || state.SagaID == "" {
		return errors.NewCode(errors.InvalidInput, "saga state is nil or empty")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.states[state.SagaID]; exists {
		return errors.NewCode(errors.Conflict, "saga already exists").WithContext("saga_id", state.SagaID)
	}

	s.states[state.SagaID] = state.Clone()
	return nil
}

// Update 更新对象并写入存储。
//
// 说明：
// - Update 更新状态。
func (s *MemorySagaStateStore) Update(ctx context.Context, state *SagaState) error {
	if state == nil || state.SagaID == "" {
		return errors.NewCode(errors.InvalidInput, "saga state is nil or empty")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.states[state.SagaID]; !exists {
		return errors.NewCode(errors.NotFound, "saga not found").WithContext("saga_id", state.SagaID)
	}

	s.states[state.SagaID] = state.Clone()
	return nil
}

// Delete 删除数据并同步到存储。
//
// 说明：
// - Delete 删除状态。
func (s *MemorySagaStateStore) Delete(ctx context.Context, sagaID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.states, sagaID)
	return nil
}

// List 从存储中查询对象。
//
// 说明：
// - List 列出状态。
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

// Clear 清空所有状态（测试用）。
func (s *MemorySagaStateStore) Clear() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.states = make(map[string]*SagaState)
}

// Count 返回状态数量（测试用）。
func (s *MemorySagaStateStore) Count() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return len(s.states)
}

// Ensure MemorySagaStateStore implements ISagaStateStore
var _ ISagaStateStore = (*MemorySagaStateStore)(nil)
