package observe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// NoopTracer 提供禁用追踪时使用的空实现。
type NoopTracer struct{}

// NewNoopTracer 创建 NoopTracer 实例。
func NewNoopTracer() *NoopTracer {
	return &NoopTracer{}
}

// StartSpan 创建新的 Span，并返回带链路信息的上下文。
func (t *NoopTracer) StartSpan(ctx context.Context, _ string, _ ...SpanOption) (context.Context, ISpan) {
	return ctx, &noopSpan{}
}

// Extract 从传输载体恢复链路上下文。
func (t *NoopTracer) Extract(ctx context.Context, _ ICarrier) context.Context { return ctx }

// Inject 把当前链路上下文写入传输载体。
func (t *NoopTracer) Inject(ctx context.Context, carrier ICarrier) {
	_ = ctx
	_ = carrier
}

type noopSpan struct{}

// End 结束当前 Span。
func (s *noopSpan) End() {}

// SetAttribute 为当前 Span 记录单个属性。
func (s *noopSpan) SetAttribute(key string, value any) {
	_ = key
	_ = value
}

// SetAttributes 为当前 Span 批量记录属性。
func (s *noopSpan) SetAttributes(attrs map[string]any) { _ = attrs }

// AddEvent 为当前 Span 追加事件。
func (s *noopSpan) AddEvent(name string, attrs ...map[string]any) {
	_ = name
	_ = attrs
}

// RecordError 为当前 Span 记录错误。
func (s *noopSpan) RecordError(err error) { _ = err }

// SetStatus 设置当前 Span 的状态。
func (s *noopSpan) SetStatus(code string, description string) {
	_ = code
	_ = description
}

// SpanContext 返回当前 Span 的链路上下文。
func (s *noopSpan) SpanContext() SpanContext { return SpanContext{} }

// 接口断言。
var _ ITracer = (*NoopTracer)(nil)
var _ ISpan = (*noopSpan)(nil)

// SimpleTracer 提供本地 Span 追踪与上下文传播。
type SimpleTracer struct {
	serviceName string
}

// NewSimpleTracer 创建 SimpleTracer 实例。
func NewSimpleTracer(serviceName string) *SimpleTracer {
	return &SimpleTracer{serviceName: serviceName}
}

// StartSpan 创建新的 Span，并返回带链路信息的上下文。
func (t *SimpleTracer) StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, ISpan) {
	cfg := &SpanConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 尝试从 context 获取父 Span
	parentSpan := spanFromContext(ctx)
	var traceID string
	var traceFlags byte
	if parentSpan != nil {
		parentCtx := parentSpan.SpanContext()
		traceID = parentCtx.TraceID
		traceFlags = parentCtx.TraceFlags
	} else {
		traceID = generateTraceID()
	}

	span := &simpleSpan{
		name:       name,
		traceID:    traceID,
		spanID:     generateSpanID(),
		traceFlags: traceFlags,
		startTime:  time.Now(),
		attrs:      cfg.Attributes,
		events:     make([]spanEvent, 0),
	}

	return contextWithSpan(ctx, span), span
}

// parseTraceparent 解析 traceparent 请求头。
func parseTraceparent(raw string) (SpanContext, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SpanContext{}, false
	}
	parts := strings.Split(raw, "-")
	if len(parts) < 4 {
		return SpanContext{}, false
	}

	version := strings.ToLower(parts[0])
	traceID := strings.ToLower(parts[1])
	parentID := strings.ToLower(parts[2])
	flags := strings.ToLower(parts[3])

	// version=00 要求恰好 4 段；更高版本允许扩展字段（此处忽略扩展）。
	if version == "00" && len(parts) != 4 {
		return SpanContext{}, false
	}
	// version=ff 是保留值（非法）。
	if version == "ff" {
		return SpanContext{}, false
	}
	if len(version) != 2 || len(traceID) != 32 || len(parentID) != 16 || len(flags) != 2 {
		return SpanContext{}, false
	}

	// 校验 hex 并排除全 0。
	vb, err := hex.DecodeString(version)
	if err != nil || len(vb) != 1 {
		return SpanContext{}, false
	}
	tb, err := hex.DecodeString(traceID)
	if err != nil || len(tb) != 16 || isAllZero(tb) {
		return SpanContext{}, false
	}
	pb, err := hex.DecodeString(parentID)
	if err != nil || len(pb) != 8 || isAllZero(pb) {
		return SpanContext{}, false
	}
	fb, err := hex.DecodeString(flags)
	if err != nil || len(fb) != 1 {
		return SpanContext{}, false
	}

	return SpanContext{
		TraceID:    traceID,
		SpanID:     parentID,
		TraceFlags: fb[0],
	}, true
}

// isAllZero 判断字节序列是否全部为零。
func isAllZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// formatTraceparent 按 W3C traceparent 规范格式化请求头。
func formatTraceparent(sc SpanContext) string {
	// 固定使用 version=00（SimpleTracer 仅实现基本传播语义）。
	return "00-" + sc.TraceID + "-" + sc.SpanID + "-" + hex.EncodeToString([]byte{sc.TraceFlags})
}

// Extract 从传输载体恢复链路上下文。
func (t *SimpleTracer) Extract(ctx context.Context, carrier ICarrier) context.Context {
	tp := carrier.Get("traceparent")
	if tp == "" {
		return ctx
	}
	sc, ok := parseTraceparent(tp)
	if !ok {
		return ctx
	}

	// 简化实现：提取 trace ID/parent span ID/flags；不做 tracestate 处理。
	span := &simpleSpan{
		traceID:    sc.TraceID,
		spanID:     sc.SpanID,
		traceFlags: sc.TraceFlags,
	}
	return contextWithSpan(ctx, span)
}

// Inject 把当前链路上下文写入传输载体。
func (t *SimpleTracer) Inject(ctx context.Context, carrier ICarrier) {
	span := spanFromContext(ctx)
	if span == nil {
		return
	}
	sc := span.SpanContext()
	if !sc.IsValid() {
		return
	}
	carrier.Set("traceparent", formatTraceparent(sc))
}

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

// 上下文 key
type spanContextKey struct{}

// contextWithSpan 把 Span 写入上下文。
func contextWithSpan(ctx context.Context, span ISpan) context.Context {
	return context.WithValue(ctx, spanContextKey{}, span)
}

// spanFromContext 从上下文提取当前 Span。
func spanFromContext(ctx context.Context) ISpan {
	span, _ := ctx.Value(spanContextKey{}).(ISpan)
	return span
}

// SpanFromContext 从上下文返回当前 Span。
func SpanFromContext(ctx context.Context) ISpan {
	return spanFromContext(ctx)
}

// generateTraceID 生成追踪 ID。
func generateTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// generateSpanID 生成新的 Span ID。
func generateSpanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// 接口断言。
var _ ITracer = (*SimpleTracer)(nil)
var _ ISpan = (*simpleSpan)(nil)
