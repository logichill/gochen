package idemp

import "sync"

// Sink 简易幂等落库器（内存），记录已处理的事件ID
type Sink struct {
	mu  sync.RWMutex
	set map[string]struct{}
}

func NewSink() *Sink { return &Sink{set: map[string]struct{}{}} }

// Seen 返回事件是否已处理
func (s *Sink) Seen(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.set[id]
	return ok
}

// Mark 标记事件为已处理
func (s *Sink) Mark(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.set[id] = struct{}{}
}
