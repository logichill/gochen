// Package observe 提供可观测性抽象（追踪、指标、日志）
//
// 设计目标：
//   - 提供统一的追踪和指标接口，支持多种后端实现。
//   - 核心仓库仅提供接口与轻量默认实现（noop/memory）
//   - 对 OpenTelemetry / Prometheus 等重依赖实现，建议使用独立扩展模块（如 gochen-contrib）
//
// 使用示例：
//
//	// 初始化追踪器（gochen-contrib）
//	tracer := oteltracer.New("my-service") // import oteltracer "gochen-contrib/observe/otel"
//
//	// 在业务代码中使用。
//	ctx, span := tracer.StartSpan(ctx, "operation.name",
//	    observe.WithAttribute("key", "value"))
//	defer span.End()
//
//	// 记录错误。
//	if err != nil {
//	    span.RecordError(err)
//	}
package observe

import (
	"context"

	"gochen/contextx/propagation"
)

// ITracer 追踪器接口。
//
// 提供分布式追踪能力，用于跟踪请求在系统中的流转路径。
type ITracer interface {
	// StartSpan 开始一个新的 Span
	//
	// name 是 Span 的名称，通常使用 "component.operation" 格式
	// 返回带有 Span 上下文的新 context 和 Span 对象
	StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, ISpan)

	// Extract 从载体中提取追踪上下文
	//
	// 用于跨进程追踪传播
	Extract(ctx context.Context, carrier propagation.IEnumerableCarrier) context.Context

	// Inject 将追踪上下文注入到载体中
	//
	// 用于跨进程追踪传播
	Inject(ctx context.Context, carrier propagation.IEnumerableCarrier)
}

// ISpan 抽象 Span 的记录与传播能力。
type ISpan interface {
	// End 结束 Span
	//
	// 必须在操作完成后调用，通常使用 defer span.End()
	End()

	// SetAttribute 设置属性
	//
	// 属性用于记录 Span 相关的上下文信息
	SetAttribute(key string, value any)

	// SetAttributes 批量设置属性
	SetAttributes(attrs map[string]any)

	// AddEvent 添加事件
	//
	// 事件用于记录 Span 生命周期中的重要时间点
	AddEvent(name string, attrs ...map[string]any)

	// RecordError 记录错误
	//
	// 将错误信息附加到 Span，并设置 Span 状态为错误
	RecordError(err error)

	// SetStatus 设置 Span 状态
	//
	// code: "ok", "error", "unset"
	SetStatus(code string, description string)

	// SpanContext 获取 Span 上下文
	//
	// 用于跨进程传播或日志关联
	SpanContext() SpanContext
}

// SpanContext 保存可跨进程传播的链路标识。
type SpanContext struct {
	TraceID    string // 追踪 ID
	SpanID     string // Span ID
	TraceFlags byte   // 追踪标志
}

// IsValid 判断有效。
func (sc SpanContext) IsValid() bool {
	return sc.TraceID != "" && sc.SpanID != ""
}

// SpanOption 定义配置 Span 的可选函数。
type SpanOption func(*SpanConfig)

// SpanConfig 汇总创建 Span 时的可选配置。
type SpanConfig struct {
	Attributes map[string]any
	Kind       SpanKind
	Links      []Link
}

// SpanKind 标识 Span 在链路中的角色。
type SpanKind int

const (
	// SpanKindUnspecified 是常量。
	SpanKindUnspecified SpanKind = iota
	// SpanKindInternal 是常量。
	SpanKindInternal
	// SpanKindServer 是常量。
	SpanKindServer
	// SpanKindClient 是常量。
	SpanKindClient
	// SpanKindProducer 是常量。
	SpanKindProducer
	// SpanKindConsumer 是常量。
	SpanKindConsumer
)

// Link 描述与当前 Span 关联的外部链路。
type Link struct {
	SpanContext SpanContext
	Attributes  map[string]any
}

func isNilCarrier(carrier propagation.IEnumerableCarrier) bool {
	if carrier == nil {
		return true
	}
	if c, ok := carrier.(propagation.MapCarrier); ok {
		return c == nil
	}
	return false
}

// WithAttribute 为新建 Span 添加单个属性。
func WithAttribute(key string, value any) SpanOption {
	return func(cfg *SpanConfig) {
		if cfg.Attributes == nil {
			cfg.Attributes = make(map[string]any)
		}
		cfg.Attributes[key] = value
	}
}

// WithAttributes 为新建 Span 批量添加属性。
func WithAttributes(attrs map[string]any) SpanOption {
	return func(cfg *SpanConfig) {
		if cfg.Attributes == nil {
			cfg.Attributes = make(map[string]any)
		}
		for k, v := range attrs {
			cfg.Attributes[k] = v
		}
	}
}

// WithSpanKind 指定新建 Span 的角色类型。
func WithSpanKind(kind SpanKind) SpanOption {
	return func(cfg *SpanConfig) {
		cfg.Kind = kind
	}
}
