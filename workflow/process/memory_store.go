package process

import (
    "context"
    "sync"
)

// MemoryStore 内存实现（用于示例/测试）
type MemoryStore struct {
    mu    sync.RWMutex
    items map[ID]*State
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{items: map[ID]*State{}} }

func (s *MemoryStore) Get(ctx context.Context, id ID) (*State, error) {
    s.mu.RLock(); defer s.mu.RUnlock()
    if st, ok := s.items[id]; ok { cp := *st; return &cp, nil }
    return nil, nil
}

func (s *MemoryStore) Save(ctx context.Context, st *State) error {
    if st == nil { return nil }
    s.mu.Lock(); defer s.mu.Unlock()
    cp := *st
    s.items[st.ID] = &cp
    return nil
}

func (s *MemoryStore) Delete(ctx context.Context, id ID) error {
    s.mu.Lock(); defer s.mu.Unlock()
    delete(s.items, id)
    return nil
}

