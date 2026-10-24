package observe

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"sync/atomic"
	"time"
)

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
		attrs:      cloneAttributes(cfg.Attributes),
		events:     make([]spanEvent, 0),
	}

	return contextWithSpan(ctx, span), span
}

var fallbackTraceSeq uint64

// cloneAttributes 浅拷贝调用方传入的属性 map；直接别名会让 span 的
// SetAttribute 与调用方持有的 map 并发读写冲突，触发 concurrent map write panic。
func cloneAttributes(attrs map[string]any) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(attrs))
	for k, v := range attrs {
		cloned[k] = v
	}
	return cloned
}

// generateTraceID 生成追踪 ID。
func generateTraceID() string {
	b := randomNonZeroBytes(16)
	return hex.EncodeToString(b)
}

// generateSpanID 生成新的 Span ID。
func generateSpanID() string {
	b := randomNonZeroBytes(8)
	return hex.EncodeToString(b)
}

func randomNonZeroBytes(size int) []byte {
	b := make([]byte, size)
	for i := 0; i < 3; i++ {
		if _, err := rand.Read(b); err == nil && !isAllZero(b) {
			return b
		}
	}

	seq := atomic.AddUint64(&fallbackTraceSeq, 1)
	var seed [24]byte
	binary.LittleEndian.PutUint64(seed[:8], uint64(time.Now().UnixNano()))
	binary.LittleEndian.PutUint64(seed[8:], seq)
	binary.LittleEndian.PutUint64(seed[16:], uint64(os.Getpid()))

	hash := sha256.New()
	_, _ = hash.Write(seed[:])
	if hostname, err := os.Hostname(); err == nil {
		_, _ = hash.Write([]byte(hostname))
	}
	block := hash.Sum(nil)
	for copied := 0; copied < len(b); {
		copied += copy(b[copied:], block)
		if copied >= len(b) {
			break
		}
		hash.Reset()
		_, _ = hash.Write(block)
		binary.LittleEndian.PutUint64(seed[:8], uint64(copied))
		_, _ = hash.Write(seed[:8])
		block = hash.Sum(block[:0])
	}
	if isAllZero(b) {
		b[len(b)-1] = 1
	}
	return b
}

// 接口断言。
var _ ITracer = (*SimpleTracer)(nil)
