package idemp

import "sync"

// Sink 简易幂等落库器（内存），记录已处理的事件ID
type Sink struct {
	mu  sync.RWMutex
	set map[string]struct{}
}

// NewSink 创建一个基于内存集合的幂等记录器。
func NewSink() *Sink { return &Sink{set: map[string]struct{}{}} }

// Seen 判断给定事件 ID 是否已经被标记为已处理。
func (s *Sink) Seen(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.set[id]
	return ok
}

// Mark 把给定事件 ID 记录为已处理。
func (s *Sink) Mark(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.set[id] = struct{}{}
}
