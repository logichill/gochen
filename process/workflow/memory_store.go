package workflow

import (
	"context"
	"sync"

	"gochen/errors"
)

// MemoryStore 内存实现（用于示例/测试）。
type MemoryStore struct {
	mu          sync.RWMutex
	definitions map[string]*Definition
	items       map[ID]*State
}

// NewMemoryStore 创建 MemoryStore。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		definitions: map[string]*Definition{},
		items:       map[ID]*State{},
	}
}

// GetDefinition 从存储中查询流程定义。
func (s *MemoryStore) GetDefinition(ctx context.Context, id string) (*Definition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if def, ok := s.definitions[id]; ok {
		return cloneDefinition(def), nil
	}
	return nil, nil
}

// SaveDefinition 保存流程定义。
func (s *MemoryStore) SaveDefinition(ctx context.Context, def *Definition) error {
	if def == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.definitions[def.ID] = cloneDefinition(def)
	return nil
}

// DeleteDefinition 删除流程定义。
func (s *MemoryStore) DeleteDefinition(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.definitions, id)
	return nil
}

// Get 从存储中查询实例状态。
func (s *MemoryStore) Get(ctx context.Context, id ID) (*State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.items[id]; ok {
		return cloneState(st), nil
	}
	return nil, nil
}

// Save 保存实例状态。
func (s *MemoryStore) Save(ctx context.Context, st *State) error {
	if st == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := cloneState(st)
	if cp.Version == 0 {
		cp.Version = 1
	}
	s.items[st.ID] = cp
	return nil
}

// SaveIfVersion 在版本匹配时保存实例状态，并原子递增版本号。
func (s *MemoryStore) SaveIfVersion(ctx context.Context, st *State, expectedVersion uint64) error {
	if st == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.items[st.ID]
	switch {
	case expectedVersion == 0:
		if exists {
			return errors.NewCode(errors.Conflict, "workflow instance already exists").
				WithContext("instance_id", string(st.ID))
		}
	case !exists:
		return errors.NewCode(errors.Conflict, "workflow instance does not exist").
			WithContext("instance_id", string(st.ID))
	case current.Version != expectedVersion:
		return errors.NewCode(errors.Conflict, "workflow instance version mismatch").
			WithContext("instance_id", string(st.ID)).
			WithContext("expected_version", expectedVersion).
			WithContext("current_version", current.Version)
	}

	cp := cloneState(st)
	cp.Version = expectedVersion + 1
	s.items[st.ID] = cp
	return nil
}

// Delete 删除实例状态。
func (s *MemoryStore) Delete(ctx context.Context, id ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
	return nil
}
