package observe

import (
	"context"
	"encoding/hex"
	"strings"

	"gochen/contextx/propagation"
)

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
func (t *SimpleTracer) Extract(ctx context.Context, carrier propagation.IEnumerableCarrier) context.Context {
	if isNilCarrier(carrier) {
		return ctx
	}
	tp, ok := carrier.Get("traceparent")
	if !ok || tp == "" {
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
func (t *SimpleTracer) Inject(ctx context.Context, carrier propagation.IEnumerableCarrier) {
	if isNilCarrier(carrier) {
		return
	}
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

// 上下文 key
type spanContextKey struct{}

// contextWithSpan 把 Span 写入上下文。
func contextWithSpan(ctx context.Context, span ISpan) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, spanContextKey{}, span)
}

// spanFromContext 从上下文提取当前 Span。
func spanFromContext(ctx context.Context) ISpan {
	if ctx == nil {
		return nil
	}
	span, _ := ctx.Value(spanContextKey{}).(ISpan)
	return span
}

// SpanFromContext 从上下文返回当前 Span。
func SpanFromContext(ctx context.Context) ISpan {
	return spanFromContext(ctx)
}
