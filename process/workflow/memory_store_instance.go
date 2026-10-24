package workflow

import (
	"context"

	"gochen/errors"
)

// Get 查询流程实例状态。
func (s *MemoryStore) Get(ctx context.Context, id ID) (*State, error) {
	if s == nil {
		return nil, errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, exists := s.states[id]
	if !exists || st == nil {
		return nil, nil
	}
	return cloneState(st), nil
}

// Save 保存或覆盖流程实例状态。
func (s *MemoryStore) Save(ctx context.Context, st *State) error {
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	if st == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.states[st.ID] = cloneState(st)
	return nil
}

// SaveIfVersion 按乐观并发版本号保存流程实例状态。
func (s *MemoryStore) SaveIfVersion(ctx context.Context, st *State, expectedVersion uint64) error {
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	if st == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.states[st.ID]
	if expectedVersion == 0 {
		if exists && current != nil {
			return errors.NewCode(errors.Conflict, "workflow instance already exists").
				WithContext("instance_id", string(st.ID))
		}
		st.Version = 1
		s.states[st.ID] = cloneState(st)
		return nil
	}

	if !exists || current == nil {
		return errors.NewCode(errors.NotFound, "workflow instance not found").
			WithContext("instance_id", string(st.ID))
	}
	if current.Version != expectedVersion {
		return errors.NewCode(errors.Conflict, "workflow instance version conflict").
			WithContext("instance_id", string(st.ID)).
			WithContext("expected_version", expectedVersion).
			WithContext("actual_version", current.Version)
	}

	st.Version = expectedVersion + 1
	s.states[st.ID] = cloneState(st)
	return nil
}

// Delete 删除流程实例状态。
func (s *MemoryStore) Delete(ctx context.Context, id ID) error {
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.states, id)
	return nil
}
