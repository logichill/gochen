package observe

import (
	"context"
	"gochen/errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSimpleTracer_StartSpan 验证 SimpleTracer StartSpan。
func TestSimpleTracer_StartSpan(t *testing.T) {
	tracer := NewSimpleTracer("test-service")
	ctx := context.Background()

	_, span := tracer.StartSpan(ctx, "test.operation",
		WithAttribute("key", "value"))
	defer span.End()

	sc := span.SpanContext()
	assert.NotEmpty(t, sc.TraceID)
	assert.NotEmpty(t, sc.SpanID)
	assert.True(t, sc.IsValid())
}

// TestSimpleTracer_NestedSpans 验证 SimpleTracer NestedSpans。
func TestSimpleTracer_NestedSpans(t *testing.T) {
	tracer := NewSimpleTracer("test-service")
	ctx := context.Background()

	// 父 Span
	ctx, parentSpan := tracer.StartSpan(ctx, "parent.operation")
	parentCtx := parentSpan.SpanContext()

	// 子 Span
	_, childSpan := tracer.StartSpan(ctx, "child.operation")
	childCtx := childSpan.SpanContext()

	// 子 Span 应该继承父 Span 的 trace ID
	assert.Equal(t, parentCtx.TraceID, childCtx.TraceID)
	// 但 span ID 应该不同
	assert.NotEqual(t, parentCtx.SpanID, childCtx.SpanID)

	childSpan.End()
	parentSpan.End()
}

func TestSimpleTracer_Traceparent_ExtractAndInject(t *testing.T) {
	tracer := NewSimpleTracer("test-service")

	in := MapCarrier{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}
	ctx := tracer.Extract(context.Background(), in)

	ctx, span := tracer.StartSpan(ctx, "op")
	defer span.End()

	out := MapCarrier{}
	tracer.Inject(ctx, out)

	got := out.Get("traceparent")
	if !strings.HasPrefix(got, "00-") {
		t.Fatalf("expected traceparent to start with 00-, got %q", got)
	}
	sc, ok := parseTraceparent(got)
	assert.True(t, ok)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", sc.TraceID)
	assert.Equal(t, span.SpanContext().SpanID, sc.SpanID)
	assert.Equal(t, byte(0x01), sc.TraceFlags)
}

func TestSimpleTracer_Extract_InvalidTraceparent_IsIgnored(t *testing.T) {
	tracer := NewSimpleTracer("test-service")
	// trace-id 全 0（非法）
	in := MapCarrier{
		"traceparent": "00-00000000000000000000000000000000-00f067aa0ba902b7-01",
	}
	ctx := tracer.Extract(context.Background(), in)
	assert.Nil(t, SpanFromContext(ctx))
}

// TestSimpleSpan_SetAttribute 验证 SimpleSpan SetAttribute。
func TestSimpleSpan_SetAttribute(t *testing.T) {
	tracer := NewSimpleTracer("test-service")
	ctx := context.Background()

	_, span := tracer.StartSpan(ctx, "test.operation")

	span.SetAttribute("string_key", "value")
	span.SetAttribute("int_key", 42)
	span.SetAttributes(map[string]any{
		"float_key": 3.14,
		"bool_key":  true,
	})

	span.End()
}

// TestSimpleSpan_AddEvent 验证 SimpleSpan AddEvent。
func TestSimpleSpan_AddEvent(t *testing.T) {
	tracer := NewSimpleTracer("test-service")
	ctx := context.Background()

	_, span := tracer.StartSpan(ctx, "test.operation")

	span.AddEvent("event1")
	span.AddEvent("event2", map[string]any{"key": "value"})

	span.End()
}

// TestSimpleSpan_RecordError 验证 SimpleSpan RecordError。
func TestSimpleSpan_RecordError(t *testing.T) {
	tracer := NewSimpleTracer("test-service")
	ctx := context.Background()

	_, span := tracer.StartSpan(ctx, "test.operation")

	err := errors.New("test error")
	span.RecordError(err)

	span.End()
}

// TestNoopTracer 验证 NoopTracer。
func TestNoopTracer(t *testing.T) {
	tracer := NewNoopTracer()
	ctx := context.Background()

	_, span := tracer.StartSpan(ctx, "test.operation",
		WithAttribute("key", "value"))

	// 应该不会 panic
	span.SetAttribute("key", "value")
	span.AddEvent("event")
	span.RecordError(errors.New("error"))
	span.End()

	sc := span.SpanContext()
	assert.False(t, sc.IsValid())
}

// TestSpanFromContext 验证 SpanFromContext。
func TestSpanFromContext(t *testing.T) {
	tracer := NewSimpleTracer("test-service")
	ctx := context.Background()

	// 没有 Span 时返回 nil
	span := SpanFromContext(ctx)
	assert.Nil(t, span)

	// 有 Span 时返回 Span
	ctx, expectedSpan := tracer.StartSpan(ctx, "test.operation")
	span = SpanFromContext(ctx)
	assert.NotNil(t, span)
	assert.Equal(t, expectedSpan.SpanContext().SpanID, span.SpanContext().SpanID)
}

// TestInMemoryMetrics_Counter 验证 InMemoryMetrics Counter。
func TestInMemoryMetrics_Counter(t *testing.T) {
	metrics := NewInMemoryMetrics()

	metrics.Counter("requests_total", 1, nil)
	metrics.Counter("requests_total", 1, nil)
	metrics.Counter("requests_total", 1, nil)

	assert.Equal(t, int64(3), metrics.CounterValue("requests_total", nil))
}

// TestInMemoryMetrics_CounterWithLabels 验证 InMemoryMetrics CounterWithLabels。
func TestInMemoryMetrics_CounterWithLabels(t *testing.T) {
	metrics := NewInMemoryMetrics()

	metrics.Counter("requests_total", 1, map[string]string{"method": "GET"})
	metrics.Counter("requests_total", 1, map[string]string{"method": "POST"})
	metrics.Counter("requests_total", 1, map[string]string{"method": "GET"})

	assert.Equal(t, int64(2), metrics.CounterValue("requests_total", map[string]string{"method": "GET"}))
	assert.Equal(t, int64(1), metrics.CounterValue("requests_total", map[string]string{"method": "POST"}))
}

// TestInMemoryMetrics_Gauge 验证 InMemoryMetrics Gauge。
func TestInMemoryMetrics_Gauge(t *testing.T) {
	metrics := NewInMemoryMetrics()

	metrics.Gauge("connections", 10, nil)
	assert.Equal(t, float64(10), metrics.GaugeValue("connections", nil))

	metrics.Gauge("connections", 15, nil)
	assert.Equal(t, float64(15), metrics.GaugeValue("connections", nil))

	metrics.Gauge("connections", 5, nil)
	assert.Equal(t, float64(5), metrics.GaugeValue("connections", nil))
}

// TestInMemoryMetrics_Histogram 验证 InMemoryMetrics Histogram。
func TestInMemoryMetrics_Histogram(t *testing.T) {
	metrics := NewInMemoryMetrics()

	metrics.Histogram("request_duration_ms", 10.5, nil)
	metrics.Histogram("request_duration_ms", 20.3, nil)
	metrics.Histogram("request_duration_ms", 15.7, nil)

	values := metrics.HistogramValues("request_duration_ms", nil)
	assert.Len(t, values, 3)
	assert.Equal(t, []float64{10.5, 20.3, 15.7}, values)
}

// TestInMemoryMetrics_Timer 验证 InMemoryMetrics Timer。
func TestInMemoryMetrics_Timer(t *testing.T) {
	metrics := NewInMemoryMetrics()

	timer := metrics.Timer("operation_duration_ms", nil)
	time.Sleep(10 * time.Millisecond)
	timer.Stop()

	values := metrics.HistogramValues("operation_duration_ms", nil)
	assert.Len(t, values, 1)
	assert.Greater(t, values[0], float64(5)) // 至少 5ms
}

// TestInMemoryMetrics_Reset 验证 InMemoryMetrics Reset。
func TestInMemoryMetrics_Reset(t *testing.T) {
	metrics := NewInMemoryMetrics()

	metrics.Counter("counter", 5, nil)
	metrics.Gauge("gauge", 10, nil)
	metrics.Histogram("histogram", 1.5, nil)

	metrics.Reset()

	assert.Equal(t, int64(0), metrics.CounterValue("counter", nil))
	assert.Equal(t, float64(0), metrics.GaugeValue("gauge", nil))
	assert.Nil(t, metrics.HistogramValues("histogram", nil))
}

// TestDefaultMetrics 验证 DefaultMetrics。
func TestDefaultMetrics(t *testing.T) {
	metrics := &DefaultMetrics{}

	// 应该不会 panic
	metrics.Counter("counter", 1, nil)
	metrics.Gauge("gauge", 10, nil)
	metrics.Histogram("histogram", 1.5, nil)

	timer := metrics.Timer("timer", nil)
	timer.Stop()
}
