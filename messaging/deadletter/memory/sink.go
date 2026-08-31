// Package memory 提供内存型 DLQ（死信）记录器实现。
package memory

import (
	"context"
	"sync"

	"gochen/messaging/deadletter"
)

// Sink 将死信记录保存在内存中（并发安全）。
//
// 适用场景：
//   - 单元测试：断言错误是否被正确收敛到 DLQ；
//   - 开发环境：快速观察失败消息（不建议用于生产持久化）。
type Sink struct {
	mu      sync.Mutex
	entries []deadletter.Entry
}

// NewSink 创建一个把死信保存在内存切片中的 sink。
func NewSink() *Sink {
	return &Sink{entries: make([]deadletter.Entry, 0)}
}

// Write 追加一条死信记录。
func (s *Sink) Write(_ context.Context, entry deadletter.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
	return nil
}

func (s *Sink) Entries() []deadletter.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]deadletter.Entry, len(s.entries))
	copy(cp, s.entries)
	return cp
}

var _ deadletter.ISink = (*Sink)(nil)
