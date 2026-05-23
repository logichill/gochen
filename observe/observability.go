package observe

import "gochen/logging"

// IObservability 聚合应用的可观测性依赖（日志/指标/追踪）。
//
// 说明：
//   - 业务代码建议依赖该接口（或按需依赖 Logger/Metrics/Tracer 的最小子集），
//     并在组合根显式注入，避免任何“全局默认单例”。
type IObservability interface {
	Logger() logging.ILogger
	Metrics() IMetrics
	Tracer() ITracer
}

// Runtime 聚合日志、指标与追踪依赖。
type Runtime struct {
	logger  logging.ILogger
	metrics IMetrics
	tracer  ITracer
}

// New 创建可观测性聚合器。
func New(logger logging.ILogger, metrics IMetrics, tracer ITracer) *Runtime {
	if logger == nil {
		logger = logging.NewStdLogger("")
	}
	if metrics == nil {
		metrics = &DefaultMetrics{}
	}
	if tracer == nil {
		tracer = &NoopTracer{}
	}
	return &Runtime{
		logger:  logger,
		metrics: metrics,
		tracer:  tracer,
	}
}

// Noop 返回一个默认“静默”可观测性实现（不会 panic；用于测试/无观测场景）。
func Noop() *Runtime {
	return New(logging.NewNoopLogger(), &DefaultMetrics{}, &NoopTracer{})
}

func (m *Runtime) Logger() logging.ILogger { return m.logger }

func (m *Runtime) Metrics() IMetrics { return m.metrics }

func (m *Runtime) Tracer() ITracer { return m.tracer }

var _ IObservability = (*Runtime)(nil)
