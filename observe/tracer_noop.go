package observe

import (
	"context"

	"gochen/contextx/propagation"
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
func (t *NoopTracer) Extract(ctx context.Context, _ propagation.IEnumerableCarrier) context.Context {
	return ctx
}

// Inject 把当前链路上下文写入传输载体。
func (t *NoopTracer) Inject(ctx context.Context, carrier propagation.IEnumerableCarrier) {
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
