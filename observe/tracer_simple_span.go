package observe

import (
	"sync"
	"time"
)

// simpleSpan 保存 SimpleTracer 运行时的 Span 状态。
type simpleSpan struct {
	name       string
	traceID    string
	spanID     string
	traceFlags byte
	startTime  time.Time
	endTime    time.Time
	attrs      map[string]any
	events     []spanEvent
	status     string
	statusMsg  string
	err        error
	mu         sync.Mutex
}

type spanEvent struct {
	name  string
	time  time.Time
	attrs map[string]any
}

// End 提供运行时能力。
func (s *simpleSpan) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.endTime.IsZero() {
		return
	}
	s.endTime = time.Now()
}

// SetAttribute 为当前 Span 记录单个属性。
func (s *simpleSpan) SetAttribute(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attrs == nil {
		s.attrs = make(map[string]any)
	}
	s.attrs[key] = value
}

// SetAttributes 为当前 Span 批量记录属性。
func (s *simpleSpan) SetAttributes(attrs map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attrs == nil {
		s.attrs = make(map[string]any)
	}
	for k, v := range attrs {
		s.attrs[k] = v
	}
}

// AddEvent 为当前 Span 追加事件。
func (s *simpleSpan) AddEvent(name string, attrs ...map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event := spanEvent{name: name, time: time.Now()}
	if len(attrs) > 0 {
		event.attrs = attrs[0]
	}
	s.events = append(s.events, event)
}

// RecordError 为当前 Span 记录错误。
func (s *simpleSpan) RecordError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
	s.status = "error"
	s.statusMsg = err.Error()
}

// SetStatus 设置当前 Span 的状态。
func (s *simpleSpan) SetStatus(code string, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = code
	s.statusMsg = description
}

// SpanContext 返回当前 Span 的链路上下文。
func (s *simpleSpan) SpanContext() SpanContext {
	return SpanContext{
		TraceID:    s.traceID,
		SpanID:     s.spanID,
		TraceFlags: s.traceFlags,
	}
}

var _ ISpan = (*simpleSpan)(nil)
